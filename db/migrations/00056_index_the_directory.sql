-- 00056 -- M11 API-8: the directory's index, employees_tenant_updated_idx (tenant_id, updated_at, id).
--
-- WHAT READS IT. GET /v1/directory's own half (db/queries/directory.sql, ListDirectoryEmployees) walks a
-- business's people in (updated_at, id) order from a position; the bound is two bare parameters, so it
-- is this index's condition in pgx's generic plan and a page reads the page, however deep in the
-- business it is (ADR 0028's API-8 note: two hundred thousand rows, the last page as cheap as the first;
-- the plan pin is internal/domain/tenant's, beside the read). holding_directory (00055) may walk it for a
-- peer the planner knows to be large. tenant_id leads (CLAUDE.md §6's index shape); id breaks a tie of
-- updated_at, so the index walks the order the page is defined in.
--
-- A FILE OF ITS OWN (not 00055's): its Down takes ACCESS EXCLUSIVE on employees (DROP INDEX), and the
-- holding tests' Down walks run 00055's Down; kept apart, they never lock employees, the table every
-- other test writes (opAtVersion skips this file too: no operator object).
--
-- Built inside goose's transaction (not CONCURRENTLY): it takes a SHARE lock on employees for the
-- build, so employee writes (panel, API, an invitation's acceptance) wait for it; taps only read
-- employees and do not. MEASURED (a disposable PostgreSQL 17, 42 M rows): 30.5-38.2 s -- the size past
-- which this needs CONCURRENTLY in a migration of its own is in deploy/README.md.
--
-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT (00054's rule, deploy/README.md): behind an open employee
-- write the build gives up with 55P03 instead of queueing every later write behind its SHARE lock
-- (TestEmployees00056_GivesUpAfterASecondBehindAnOpenWrite).

-- +goose Up
SET LOCAL lock_timeout = '1s';
CREATE INDEX employees_tenant_updated_idx ON public.employees (tenant_id, updated_at, id);

-- +goose Down
SET LOCAL lock_timeout = '1s';
DROP INDEX public.employees_tenant_updated_idx;
