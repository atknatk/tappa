-- daycutoff.sql -- the PANEL's reads behind the business-day settings screen (M15-07,
-- ADR 0032 §A.7): which people take their cutoff from a level, which shifts they work,
-- and which venues they tapped at lately. READS ONLY. The four writes are M15-02's
-- setters (SetTenantDayCutoff, SetLocationDayCutoff, SetDepartmentDayCutoff,
-- SetEmployeeDayCutoff); this file adds none.
--
-- THE HIERARCHY IS RESOLVED IN GO HERE, NOT IN SQL, AND THAT IS NOT A SECOND COPY OF
-- GetEmployeeForTap's coalesce. The tap asks "what is THIS person's cutoff" and gets one
-- value. This screen asks "who inherits from THIS level, and what would each of them get
-- if the level changed" -- the same coalesce evaluated with one input replaced, which a
-- single resolved column cannot answer. internal/domain/tenant's DB test holds the two
-- equal for every person it builds (the person card's value against effective_day_cutoff).
--
-- Every statement carries its own @tenant_id predicate (CLAUDE.md §4.5, belt) under RLS
-- (braces), and every join repeats the employee's or the row's tenant_id.

-- name: GetDayCutoffBusiness :one
-- The business level: its stored cutoff (NOT NULL -- the end of every coalesce), the
-- zone every wall clock of this screen is read in, and the name the screen gives it.
SELECT t.name, t.timezone, t.day_cutoff
FROM tenants t
WHERE t.id = @tenant_id;

-- name: ListDayCutoffVenues :many
-- Every venue with its stored cutoff (NULL = inherit) and its own shift. The list is
-- bounded by the buildings a business runs (the locations section's ceiling argument,
-- venuePageLimit); row_limit is the caller's ceiling and reaching it is reported, never
-- silently cut.
SELECT l.id, l.name, l.day_cutoff, l.shift_start, l.shift_end, l.overnight
FROM locations l
WHERE l.tenant_id = @tenant_id
ORDER BY l.name, l.id
LIMIT @row_limit;

-- name: ListDayCutoffDepartments :many
-- Every department with its venue, its stored cutoff (NULL = inherit) and its own shift.
SELECT d.id, d.location_id, d.name, d.day_cutoff, d.shift_start, d.shift_end, d.overnight
FROM departments d
WHERE d.tenant_id = @tenant_id
ORDER BY d.name, d.id
LIMIT @row_limit;

-- name: ListDayCutoffPeople :many
-- The people a level's cutoff can reach: everybody who is not deactivated (a person who
-- has left taps nothing), with their profile branch (employees.location_id -- NULLABLE,
-- 00037: an API-managed person has none and the branch level is skipped), their
-- department and their own override. Which of them actually INHERIT from a level is
-- decided in Go from these four facts; the three optional filters only narrow the read
-- to the level asked about (a branch, a department, one person) and are not the rule.
SELECT e.id, e.location_id, e.department_id, e.day_cutoff
FROM employees e
WHERE e.tenant_id = @tenant_id
  AND e.status <> 'deactivated'
  AND (sqlc.narg('location_id')::uuid IS NULL OR e.location_id = sqlc.narg('location_id')::uuid)
  AND (sqlc.narg('department_id')::uuid IS NULL OR e.department_id = sqlc.narg('department_id')::uuid)
  AND (sqlc.narg('employee_id')::uuid IS NULL OR e.id = sqlc.narg('employee_id')::uuid)
ORDER BY e.id
LIMIT @row_limit;

-- name: ListDayCutoffTouchedVenues :many
-- The venues each of these people TAPPED at since @since (ADR 0032 §A.7 (2), the named
-- window tenant.CutoffTouchedWindow): a cutoff comes from the profile branch, but the
-- work may be done elsewhere -- a Sliema-profile worker on Rusty Bar's 18:00-02:00 night
-- is cut by a 01:00 Sliema cutoff every night. Read from the person's own transactions
-- rows, tap channels only (a manager's typed record is not a tap). A holding partner's
-- venue (location_id NULL, site_location_id set -- K-30) is another business's row and
-- is not read: RLS hides it and this statement does not try.
SELECT DISTINCT t.employee_id, t.location_id
FROM transactions t
WHERE t.tenant_id = @tenant_id
  AND t.employee_id = ANY(@employee_ids::uuid[])
  AND t.occurred_at >= @since
  AND t.channel <> 'manual'
  AND t.location_id IS NOT NULL
ORDER BY t.employee_id, t.location_id;
