-- punchclaims.sql -- an employee's claim of the missing half of a punch (M15-15, ADR 0032 §C.2-§C.3;
-- migration 00068). The window, the K4 rule, the lock and the decision are M15-16's
-- (internal/domain/claim); this file holds the one statement the schema card owns: the claim's
-- INSERT, BUILT FROM THE ANCHOR ROW.

-- name: InsertPunchClaim :one
-- ADR 0032 §C.3: the anchor id arrives from the client (inside the signed claim context) and is not
-- trusted on its own. So the row is not VALUES from parameters: it is SELECTed from the anchor,
-- under its tenant AND its person -- tenant_id and employee_id are the anchor's own columns, read
-- with the explicit predicate (§4.5's belt beside RLS's braces). Another tenant's anchor, another
-- person's anchor or no anchor at all is ZERO ROWS -- pgx.ErrNoRows, nothing written -- never a
-- claim on a row that is not the employee's.
--
-- What the anchor must otherwise BE (directed, the opposite direction, a business day, not a
-- practice row, the claimed moment on its open side) is migration 00068's trigger
-- (tappa_check_punch_claim, 23514); whether it is STILL open / still an orphan, and whether the
-- moment is inside the window, is the caller's under LockEmployeeForTap. A second claim on the same
-- anchor is 23505 (punch_claims_one_per_anchor) -- M15-17 answers it with "Sent".
--
-- prefilled_at is NULL when the form offered no real shift end inside the window (ADR 0032 §C.2).
-- created_at is the database's clock (tappa_app may not write it -- 00068's grant).
INSERT INTO punch_claims (tenant_id, employee_id, anchor_transaction_id, kind, claimed_at, prefilled_at)
SELECT a.tenant_id, a.employee_id, a.id, @kind::text, @claimed_at::timestamptz,
       sqlc.narg('prefilled_at')::timestamptz
  FROM transactions AS a
 WHERE a.id = @anchor_transaction_id
   AND a.tenant_id = @tenant_id
   AND a.employee_id = @employee_id::uuid
RETURNING id, created_at;
