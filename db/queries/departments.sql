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
