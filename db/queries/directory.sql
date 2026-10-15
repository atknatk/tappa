-- directory.sql -- M11 API-8, GET /v1/directory: the business's OWN half of the directory (ADR 0028
-- §2, §9). The peers' half is holding_directory (migration 00055 -- ADR 0027's closed list), called
-- by hand in internal/db/holding.go; the two are read in ONE transaction of the key's tenant
-- (WithTenant + the explicit tenant_id filter of every statement here -- CLAUDE.md §4.5) and merged
-- on (updated_at, id) by internal/domain/tenant (employeedirectory.go).
--
-- No statement here names transactions, and none reads the e-mail (K-1f, K-24): a directory row is
-- an identity, a code, two names, the active flag and updated_at.

-- name: GetDirectoryHead :one
-- The walk's clock and the business's company code. as_of is the TRANSACTION's start
-- (transaction_timestamp): it is taken before any snapshot this transaction reads, so a change that
-- the reads below do not see either committed after it or was written by a transaction still open at
-- it -- the integrator's next walk starts at as_of minus K-21's ten minutes (employeedirectory.go
-- says what that guarantees). One row: the key's tenant exists.
SELECT transaction_timestamp()::timestamptz AS as_of, t.api_code
FROM tenants t
WHERE t.id = @tenant_id;

-- name: ListDirectoryEmployees :many
-- The business's employees after a position in (updated_at, id) order -- live and deactivated, panel-
-- made and API-made -- at most page_size of them. The first page of a walk passes (since - 1 µs, the
-- greatest uuid): "after" it is exactly updated_at >= since, because timestamptz counts microseconds
-- and no id is above the greatest one; a walk with no since passes the year 1 (tenant.directoryOrigin
-- -- updated_at is 00037's trigger clock, never that early).
--
-- 🔴 THE BOUND IS TWO BARE PARAMETERS, NOTHING COMPUTED: it is an index condition of
-- employees_tenant_updated_idx (00056) in a GENERIC plan -- the one pgx's prepared statement settles
-- on -- only so. Measured (API-8, one business of two hundred thousand rows, a page 150 000 rows in): with
-- `coalesce(@after_at, '-infinity')` in its place the generic plan read the index from the business's
-- first row and filtered 150 001 rows away (90 ms a page); bare, it reads the page (0.8 ms).
-- TestEmployeeDirectory_TheBoundIsTheIndexsNotAFilter pins it.
SELECT e.id, e.external_ref, e.employee_code, e.first_name, e.last_name,
       (e.status <> 'deactivated')::boolean AS active, e.updated_at
FROM employees e
WHERE e.tenant_id = @tenant_id
  AND (e.updated_at, e.id) > (@after_at::timestamptz, @after_id::uuid)
ORDER BY e.updated_at, e.id
LIMIT @page_size;
