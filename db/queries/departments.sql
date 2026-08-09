-- departments.sql -- tenant-scoped department reads. Every query carries an
-- explicit tenant_id filter (CLAUDE.md section 4.5, belt + braces on RLS) and
-- runs inside db.(*DB).WithTenant.
--
-- It is a SEPARATE file from locations.sql even though migration 00002 creates
-- both tables, because that file states in its header that it holds location
-- reads and a department query underneath it would make the header false.

-- name: GetDepartmentShift :one
-- The DEPARTMENT shift, which BEATS the location shift when the employee has a
-- department (CLAUDE.md section 5, Q17, M4-05): a kitchen starts before the
-- counter does, and judging a kitchen hand against the venue's opening time would
-- report lateness that is not there.
--
-- The id comes from employees.department_id -- a value the DATABASE produced, not
-- one a client chose. Returns NO ROW for a department of another tenant (explicit
-- predicate + RLS), and the caller treats that exactly as it treats an employee
-- with no department at all: fall back to the tapped location's shift. Lateness
-- is a REPORT output and never changes a verdict (section 5), so the fallback
-- cannot cost anyone a record.
--
-- shift_start/shift_end are nullable: a department with no shift means lateness
-- is NOT COMPUTED for it, which is different from "on time" and is why the domain
-- carries a nil Shift rather than a zero one.
SELECT id, tenant_id, name, shift_start, shift_end, overnight
FROM departments
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: ListDepartmentsForTenant :many
-- The tenant's departments, for the panel's DEPARTMENT filter (M6-03).
--
-- It exists beside GetDepartmentShift rather than widening it, which is the split
-- employees.sql already argues for: that query answers a DECISION question (whose
-- shift judges lateness) and returns no name it does not need; this one answers a
-- DISPLAY question and returns the name it exists for. Merging them would make one
-- query serve two audiences and the stricter one would lose.
--
-- Ordered by location then name so the filter reads the way the business is laid
-- out -- a venue and the departments inside it -- rather than alphabetically
-- across venues.
SELECT d.id, d.tenant_id, d.location_id, d.name, l.name AS location_name
FROM departments d
JOIN locations l ON l.tenant_id = d.tenant_id AND l.id = d.location_id
WHERE d.tenant_id = @tenant_id
ORDER BY l.name, d.name;

-- ---------------------------------------------------------------------------
-- THE DEPARTMENTS HALF OF THE LOCATIONS SECTION (M6-06 phase A).
--
-- The two queries above are the DECISION read (whose shift judges lateness) and
-- the panel's FILTER read. The four below are the management screen's: they
-- return the shift columns the filter has no use for, and they write.
--
-- 🔴 A DEPARTMENT'S SHIFT BEATS ITS VENUE'S (section 5, Q17, M4-05), so these
-- writes decide which wall clock somebody is judged against. NULL means lateness
-- is NOT COMPUTED, not that the shift starts at midnight -- the same distinction
-- GetDepartmentShift's comment above makes, now on the write side where it can be
-- destroyed. internal/domain/tenant/venue.go records before and after in
-- audit_log inside the same transaction.
--
-- TENANT SCOPE (section 4.5, belt + braces on RLS): every statement below binds
-- tenant_id to the caller's parameter as a top-level conjunct of its own WHERE.

-- name: ListPanelDepartments :many
-- Every department in the business with its venue and its shift.
--
-- IT IS NOT ListDepartmentsForTenant, which is the panel's FILTER source and is
-- left exactly as it is: that one returns no shift because a filter has no use for
-- one, and widening it would make every filtered page in two other sections carry
-- three columns nobody reads.
--
-- ORDERED BY VENUE THEN NAME, the same order the filter uses, so the management
-- screen reads the way the business is laid out -- a venue and the departments
-- inside it -- rather than alphabetically across venues.
SELECT d.id, d.tenant_id, d.location_id, d.name,
       d.shift_start, d.shift_end, d.overnight, d.created_at,
       l.name AS location_name
FROM departments d
JOIN locations l ON l.tenant_id = @tenant_id AND l.id = d.location_id
WHERE d.tenant_id = @tenant_id
ORDER BY l.name, d.name, d.id
LIMIT @row_limit;

-- name: GetPanelDepartment :one
-- One department, for the edit form and for the BEFORE half of the audit row --
-- read inside the write's transaction, for the reason GetPanelLocation gives.
SELECT d.id, d.tenant_id, d.location_id, d.name,
       d.shift_start, d.shift_end, d.overnight, d.created_at,
       l.name AS location_name
FROM departments d
JOIN locations l ON l.tenant_id = @tenant_id AND l.id = d.location_id
WHERE d.tenant_id = @tenant_id
  AND d.id = @id;

-- name: CreateDepartment :one
-- Adds a department INSIDE a venue.
--
-- 🔴 THE VENUE IS SELECTED, NOT ASSIGNED, which is the shape MoveEmployee argues
-- for at length in db/queries/employees.sql: the SELECT matches the location
-- inside the caller's tenant, so a foreign or invented location_id produces ZERO
-- ROWS -- a refusal the manager can be shown a sentence about, rather than a
-- 23503 the handler has to translate from a driver error string.
--
-- ⚠️ AND IT IS THE SECOND DEFENCE, NOT THE ONLY ONE. departments_location_fk is a
-- COMPOSITE key on (location_id, tenant_id) -> locations (id, tenant_id)
-- (migration 00002), so a cross-tenant pairing is structurally impossible even
-- with every predicate below deleted. What the predicate buys is the difference
-- between a refusal and an error, which is what section 4.6 asks for on a screen
-- somebody is standing in front of.
--
-- NULL SHIFTS PASS THROUGH AS NULL. See CreateLocation: a zero is not an absence,
-- and departments_shift_pair refuses a half-filled pair but cannot refuse 00:00.
INSERT INTO departments (
    tenant_id, location_id, name, shift_start, shift_end, overnight
)
SELECT @tenant_id, l.id, @name,
       sqlc.narg('shift_start'), sqlc.narg('shift_end'), @overnight
FROM locations l
WHERE l.tenant_id = @tenant_id
  AND l.id = @location_id
RETURNING id, tenant_id, location_id, name, shift_start, shift_end, overnight, created_at;

-- name: UpdateDepartment :one
-- Rewrites a department's name and shift.
--
-- 🔴 location_id IS NOT IN THE SET LIST, AND THAT IS A DECISION WITH A REASON
-- RATHER THAN A MISSING FEATURE. Moving a department to another venue would leave
-- every employee already in it holding a (venue, department) pair that no longer
-- agrees -- exactly the mismatch a security audit found MoveEmployee accepting,
-- and which it now refuses with `d.location_id = l.id`. So after such a move those
-- people could not be saved by the employees screen at all, and until somebody
-- noticed they would be judged against this venue's shift while working at that
-- one (section 5). Repairing them would need an UPDATE over employees inside this
-- statement, which is a different task with its own audit obligations. The screen
-- says the venue is fixed rather than offering a control that quietly does this.
--
-- EVERY OTHER COLUMN IS ASSIGNED, including the shift being CLEARED -- the reason
-- UpdateLocation gives: a partial update needs "leave this alone" to be distinct
-- from "make this NULL", and one shape for both is how a clear becomes a no-op.
UPDATE departments
SET name        = @name,
    shift_start = sqlc.narg('shift_start'),
    shift_end   = sqlc.narg('shift_end'),
    overnight   = @overnight
WHERE tenant_id = @tenant_id
  AND id = @id
RETURNING id, tenant_id, location_id, name, shift_start, shift_end, overnight, created_at;

-- name: CountDepartmentReferences :one
-- What still points at this department. Two keys rather than the venue's four:
-- employees_department_fk and transactions_department_fk, both ON DELETE RESTRICT.
-- 🔴 @id IS CAST EXPLICITLY, AND THE CAST IS LOAD-BEARING RATHER THAN DECORATIVE.
-- employees.department_id and transactions.department_id are both NULLABLE, so
-- without the cast sqlc infers the PARAMETER as nullable too and emits *uuid.UUID.
-- A nil then means "count the rows whose department_id IS NULL" -- every employee in
-- the business who is not in a department -- which is a different question that would
-- answer "in use" for a department that is not. Measured: sqlc v1.28 emitted exactly
-- that before the cast was added.
SELECT
    (SELECT count(*) FROM employees e
      WHERE e.tenant_id = @tenant_id AND e.department_id = @id::uuid) AS employee_count,
    (SELECT count(*) FROM transactions x
      WHERE x.tenant_id = @tenant_id AND x.department_id = @id::uuid) AS transaction_count;

-- name: DeleteDepartment :one
-- Removes a department that nothing points at. Same argument as DeleteLocation: the
-- foreign keys are the guard, the count is what the screen says.
DELETE FROM departments
WHERE tenant_id = @tenant_id
  AND id = @id
RETURNING id, location_id, name, shift_start, shift_end, overnight, created_at;
