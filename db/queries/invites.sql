-- invites.sql -- the TENANT-SCOPED half of activation (M5-02): issuing an invite,
-- consuming it exactly once, and flipping the employee to 'active'.
--
-- Every query below is TENANT-SCOPED: each one names tenant_id explicitly
-- (CLAUDE.md section 4.5, belt + braces on top of RLS) and each is meant to run
-- inside db.(*DB).WithTenant, i.e. with app.tenant_id established. Precisely: the
-- three that READ or MUTATE existing rows carry tenant_id as a WHERE predicate;
-- CreateInvite is an INSERT, so it carries tenant_id as a VALUE instead -- there
-- is no row to filter yet, and the RLS WITH CHECK is what refuses a value that
-- disagrees with the context (the composite FK refuses a foreign employee on top).
-- The belt is the same in both shapes; only the syntax differs.
--
-- There is exactly ONE invite lookup in the codebase that carries no tenant scope
-- at all -- the context-less resolution path GetInviteByCodeHash
-- (db/queries/resolve.sql, ADR 0002 madde 7) -- because there the tenant is the
-- RESULT of the lookup, not an input. It is deliberately NOT duplicated here.
--
-- KIRMIZI CIZGI (section 4.7): the raw invite CODE appears nowhere in this file
-- and is not stored. Only code_hash (a hex HMAC-SHA256 computed by phase B with
-- the internal/session key pattern, SHAPE-enforced by a CHECK in 00009) is written
-- or matched, and NO query below RETURNS code_hash -- greppable: the column name
-- occurs in no SELECT or RETURNING list in db/queries. The column exists to be
-- matched, not to be handed back to Go, so a row read cannot leak it into a log.
-- The limit is stated in 00009 and repeated here because it matters at the call
-- site: the CHECK proves the value's SHAPE, never its PROVENANCE -- computing it
-- as an HMAC under a server-side key is the caller's obligation, not the
-- database's.
--
-- NO DELETE QUERIES BY DESIGN: migration 00009 REVOKEs DELETE on employee_invites
-- from tappa_app, so no application code path can destroy an invite row (section
-- 4.6). A consumed or expired invite stays readable as the audit answer to "which
-- code activated this person, and when".
--
-- THE TWO LIMITS OF THAT GUARANTEE, stated precisely (the sessions.sql wording
-- applies here for the same reasons).
--
--   1. DELETE is blocked for tappa_app -- that is, for every code path that serves
--      a request -- and NOT for tappa_owner, the migration superuser, which keeps
--      DELETE and bypasses RLS by definition (measured, not assumed). Closing that
--      too needs a BEFORE DELETE trigger in a NEW migration; 00005 does exactly
--      that for transactions/audit_log, but its trigger also blocks UPDATE, which
--      this table cannot afford (used_at). Deliberate non-goal, matching the
--      sessions/employees precedent.
--   2. WHICH COLUMN may be written is enforced by the DATABASE; WHAT VALUE it may
--      take is not. 00009 grants tappa_app column-level `UPDATE (used_at)` only, so
--      resurrecting an expired invite (expires_at), reassigning it to another
--      employee (employee_id) and rewriting its hash are all `permission denied` --
--      measured, not merely absent from this file. What a column grant cannot
--      express is a VALUE transition: `SET used_at = NULL` (un-consume) and
--      `SET used_at = <another instant>` remain permitted by the privilege system.
--      Against those the protection is that NO QUERY IN THIS FILE DOES IT --
--      ConsumeInviteAndActivate only ever moves used_at from NULL to now() -- which
--      is this file's discipline, not the database's. Anyone adding a query here
--      must keep it; making it structural needs a trigger in a NEW migration.

-- name: CreateInvite :one
-- Issues an invitation for one employee. tenant_id is supplied explicitly and the
-- RLS WITH CHECK confirms it equals the transaction context, so a mismatched
-- tenant is refused by the DATABASE, not merely by the caller. The composite FK
-- additionally refuses an employee_id belonging to another tenant.
--
-- code_hash is the HMAC of a code this query never sees. expires_at is REQUIRED
-- (NOT NULL) and cannot be 'infinity' (CHECK employee_invites_expiry_finite), so
-- neither an omitted nor a literally-never expiry can be written. A finite but
-- absurd expiry (year 9999) IS still writable -- that is a TTL policy question and
-- the TTL value is the caller's (phase B) decision; see 00009 for the exact limit.
--
-- code_hash is written, never returned (section 4.7).
INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
VALUES (@tenant_id, @employee_id, @code_hash, @expires_at)
RETURNING id, tenant_id, employee_id, created_at, expires_at, used_at;

-- name: ConsumeInviteAndActivate :one
-- THE activation statement, and the most critical query in M5-02. ONE statement
-- does BOTH halves -- consume the invite and flip the employee to 'active' -- so
-- that "this employee was activated" IMPLIES "an invite was consumed for them".
--
-- WHY THEY ARE FUSED (third-eye finding, 2026-07-31). Two separate queries made
-- that implication a property of the CALL ORDER, not of the schema: a phase-B
-- code path could activate any 'invited'/'active' employee in the tenant WITHOUT
-- consuming anything, which is precisely the "manager activates a ghost employee"
-- shape the M5-02 card warns about. There is deliberately NO standalone activate
-- query and NO standalone consume query in this file.
--
-- THE SCOPE OF THAT, exactly: this is the only statement in db/queries that writes
-- employees.status (greppable -- `UPDATE employees` appears once in the whole
-- directory, here). It is NOT a privilege-level guarantee: 00003 grants tappa_app
-- table-wide UPDATE on employees, which it needs for the rest of the employee
-- lifecycle, so hand-written SQL outside the generated store could still flip the
-- column. What this buys is that the generated Querier -- the only DB surface
-- handlers are supposed to touch (CLAUDE.md section 3: no raw SQL in handlers) --
-- offers no way to activate without spending an invite.
--
-- KEYED BY code_hash, NOT BY invite id -- ON PURPOSE. An id-keyed consumption
-- would make KNOWING THE CODE UNNECESSARY: ListPendingInvitesForEmployee hands the
-- id to any caller in the tenant, so an endpoint taking an id from a request body
-- would activate an employee for someone who never had the link. Matching on the
-- HASH means the caller must present a value that -- given phase B derives it as
-- an HMAC under a server-side key, as internal/session does -- only the code's
-- holder can produce. The binding is exactly as strong as that derivation, which
-- is why the derivation is not optional.
-- (Cancelling an invite from the panel -- which legitimately knows only the id --
-- is a DIFFERENT operation and must NOT reuse used_at: that stamp means "the code
-- was used", and overloading it would corrupt the audit answer. Cancellation gets
-- its own column in a NEW migration when M6 needs it.)
--
-- WHY ATOMICITY MATTERS. A read ("is used_at NULL?") followed by a write ("stamp
-- it") is a race: two requests carrying the SAME code -- the link opened on two
-- devices, or replayed deliberately -- can both read NULL and both stamp,
-- producing TWO activations and TWO session cookies for one invitation. The
-- consumed CTE is a single UPDATE whose WHERE carries the state test, so under
-- READ COMMITTED the second one blocks on the row lock, re-reads the committed
-- row, finds `used_at IS NULL` false, updates 0 rows and returns nothing. EXACTLY
-- ONE caller can win. Proven with N concurrent goroutines under -race in
-- internal/db/invites_test.go, next to a negative control showing the naive
-- read-then-write producing many winners.
--
-- ORDER OF EFFECTS, stated exactly. A data-modifying CTE always executes, even
-- when the outer statement matches no row -- so the ONLY thing that keeps a
-- non-activatable employee's invite from being consumed is the EXISTS guard below
-- (see the two-tests note). With it, a deactivated employee yields 0 rows from the
-- CTE, 0 rows from the outer UPDATE, no stamp at all, and sqlc returns
-- pgx.ErrNoRows. This now holds whether the caller propagates the error or
-- swallows it and commits -- both were measured. Propagating remains correct (the
-- activation did not happen) and db.WithTenant's rollback is still a second belt,
-- but the invite's survival no longer depends on it.
--
-- Both invite failure modes collapse into "0 rows" on purpose: an already-consumed
-- invite and an expired one are equally unusable and the caller MUST NOT be able to
-- tell them apart from this outcome (that would be an oracle). When the difference
-- matters -- phase B's "already active: new phone or attack?" decision -- it comes
-- from the resolver, which returns used_at and expires_at as FACTS without
-- filtering (00009), and the caller decides.
--
-- WHY 'invited' OR 'active' AND NOT 'deactivated': re-activating a deactivated
-- employee through an invite would be a privilege-escalation path around the
-- manager-only deactivation decision (section 5 row 4 rejects that person's taps).
-- 'active' IS included so a SECOND activation (a new phone) is not an error at this
-- layer; the "new device or attack?" decision belongs to phase B
-- (ListPendingInvitesForEmployee + ListSessionsForEmployee +
-- RevokeSessionsForEmployee, M5-01).
--
-- activated_at IS NEVER OVERWRITTEN -- COALESCE keeps the FIRST stamp. Two reasons,
-- stated at the strength each one actually has:
--
--   1. It is an audit stamp: "when this person first came online". Rewriting it on
--      a later, unrelated event is a quiet history edit, which is what section 4.6
--      exists to prevent, and every reader (reports, review queue, the M6 anomaly
--      list) takes it to mean the first activation.
--   2. It is a load-bearing INPUT to the practice (TRAINING) rule: tap.isPracticeTap
--      (M4-06) marks a tap as practice -- never counted toward worked hours -- when
--      Employee.ActivatedAt is non-zero AND the person has no prior tap. MEASURED
--      LIMIT, not a claim: because that rule ALSO requires no prior tap, resetting
--      activated_at does NOT by itself hand a second practice tap to someone who has
--      already tapped (internal/domain/tap/decide.go, isPracticeTap). So this
--      COALESCE is not what closes the hours-inflation hole today -- the "no prior
--      tap" condition is. It keeps the field's MEANING stable for the rule that reads
--      it. It does not by itself PREVENT a future change from re-opening that hole
--      -- nothing in SQL could -- it removes the silent way to do it.
--
-- CALL ORDER (phase B): GetInviteByCodeHash (context-less, ADR 0002 madde 7)
-- recovers the tenant -> WithTenant establishes app.tenant_id -> THIS statement
-- runs inside that transaction -> the returned employee id is what the session is
-- issued for. The explicit tenant_id predicates are kept on BOTH halves (section
-- 4.5 belt + braces); they are satisfiable because the tenant is known by then and
-- is not an input the client supplies.
--
-- THE EMPLOYEE STATUS IS TESTED TWICE, AND THAT IS NOT A DUPLICATE. The two tests
-- guard two different writes, so removing either one loses something real:
--
--   * The EXISTS inside the CTE guards THE STAMP. A data-modifying CTE runs
--     unconditionally -- it does not care whether the outer statement matches --
--     so without it a deactivated employee's invite got consumed while nobody was
--     activated. Nothing was corrupted (the outer UPDATE returned 0 rows and the
--     caller sees pgx.ErrNoRows), but the code was BURNED, and the employee stayed
--     locked out until a manager issued a new one. That was survivable only because
--     WithTenant rolls the transaction back on error -- i.e. the protection was the
--     CALLER'S discipline. One `row, _ := q.ConsumeInviteAndActivate(...)` would
--     have burned it (measured under COMMIT). The EXISTS makes the stamp
--     conditional in the DATABASE, so no caller -- careless or not -- can spend an
--     invite belonging to an employee who is ALREADY non-activatable when the
--     statement runs. (Not "can never spend an invite for nothing": see the race
--     window below, which is narrower and is documented rather than claimed away.)
--   * The status test in the OUTER statement guards THE ACTIVATION, and it is a
--     RACE guard, not a repeat. Under READ COMMITTED the outer UPDATE re-evaluates
--     its WHERE against the row version committed by any concurrent transaction it
--     waited on. If someone deactivates the employee between the CTE and the outer
--     UPDATE, the EXISTS has already passed on the older snapshot; only the outer
--     `status IN (...)` sees the new row and refuses. Drop it and a just-deactivated
--     employee gets reactivated.
--
-- THE WINDOW THAT REMAINS, measured rather than waved away. In exactly that race --
-- a deactivation committing BETWEEN the CTE's EXISTS and the outer UPDATE's
-- re-check -- the stamp IS written while the activation is refused, so a caller
-- that commits anyway spends the code for nothing (measured with two concurrent
-- sessions: outer UPDATE 0 rows, employee still 'deactivated', used_at set). What
-- is NOT possible in that window is a deactivated employee becoming active; the
-- failure mode is a wasted invite, not an unauthorised activation, and propagating
-- the error (WithTenant's rollback) still saves the code. The steady-state case --
-- an employee who is ALREADY deactivated when the request arrives, which is the one
-- a careless caller hits deterministically -- is fully closed by the EXISTS.
--
-- Closing the race too would mean locking the employee row from inside the CTE
-- (`EXISTS (... FOR SHARE)`). Postgres accepts it, and it was REJECTED after
-- measuring what it costs: both halves of this statement would then touch the same
-- employee row as SHARE-then-EXCLUSIVE, so two concurrent activations of the SAME
-- employee (two devices, two valid invites -- a case the card explicitly expects)
-- deadlock on the lock upgrade. Measured: `ERROR: deadlock detected` (40P01), one
-- transaction aborted. Trading a wasted code for a deadlock class that surfaces as
-- a 500 is a bad trade, so the window is documented instead of closed.
--
-- This is the ONLY statement in db/queries that writes employee_invites.used_at
-- (greppable), which is what makes the un-consume limit in the header hold.
WITH consumed AS (
    UPDATE employee_invites
    SET used_at = now()
    WHERE code_hash = @code_hash
      AND tenant_id = @tenant_id
      AND used_at IS NULL
      AND now() < expires_at
      AND EXISTS (SELECT 1 FROM employees e
                  WHERE e.id = employee_invites.employee_id
                    AND e.tenant_id = employee_invites.tenant_id
                    AND e.status IN ('invited', 'active'))
    RETURNING id, tenant_id, employee_id
)
UPDATE employees e
SET status       = 'active',
    activated_at = COALESCE(e.activated_at, now())
FROM consumed c
WHERE e.id = c.employee_id
  AND e.tenant_id = c.tenant_id
  AND e.tenant_id = @tenant_id
  AND e.status IN ('invited', 'active')
RETURNING e.id, e.tenant_id, e.location_id, e.department_id, e.full_name,
          e.status, e.invited_at, e.activated_at, c.id AS invite_id;

-- name: ListPendingInvitesForEmployee :many
-- The invites of one employee that are still usable RIGHT NOW: not consumed and
-- not expired. Phase B reads this for the "this employee is already active --
-- is this a new phone or an attack?" decision, and the panel uses it to show
-- whether an invitation is outstanding.
--
-- "Pending" is defined by the same two conditions ConsumeInviteAndActivate tests,
-- so a row listed here is one that could be consumed at this instant (a row can
-- still expire or be consumed between the two statements -- the activation
-- statement is the authority, this is a view). Consumed and expired rows are excluded on purpose;
-- they are NOT lost (nothing deletes them, section 4.6) and a future history view
-- gets its own query rather than a filter flag on this one.
--
-- used_at is not selected: the WHERE pins it to NULL for every returned row.
-- code_hash is not selected either (section 4.7).
SELECT id, tenant_id, employee_id, created_at, expires_at
FROM employee_invites
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND used_at IS NULL
  AND now() < expires_at
ORDER BY created_at DESC;
