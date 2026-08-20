-- reviews.sql -- the FLAGGED approval queue (M6-04): two reads and one write.
--
-- 🔴 SECTION 4.3 IS THE WHOLE DESIGN OF THIS FILE. A manager approving or
-- rejecting a flagged record does NOT touch `transactions`. There is no UPDATE and
-- no DELETE here and there cannot be one: tappa_app holds SELECT + INSERT only on
-- all three tables (measured with has_table_privilege: UPDATE f, DELETE f on
-- transactions, transaction_reviews and audit_log), and migration 00005 puts a
-- BEFORE UPDATE OR DELETE trigger on top of that so even tappa_owner is refused.
-- The decision is a NEW ROW in transaction_reviews plus a NEW ROW in audit_log,
-- and the tap record keeps saying exactly what the engine decided at the time.
--
-- WHAT "PENDING" MEANS, AND WHY IT IS NOT transactions.queued. The column exists
-- and the tap engine sets it (checkin.go: queued = verdict is flag), but it is a
-- SECOND REPRESENTATION of a fact the reviews table already owns, and because
-- transactions is immutable it can never be cleared -- a reviewed record would go
-- on claiming to be queued forever. It also already disagrees with the verdict in
-- the development database: measured, 31 193 rows carry verdict='flag' and only
-- 8 928 of them carry queued=true (the other 22 265 were written directly by test
-- fixtures). So the queue is defined as the set the DATABASE will actually accept
-- a review for -- verdict='flag' AND no review row yet -- which is the same
-- predicate migration 00005's tappa_check_transaction_review trigger enforces.
--
-- 🔴 THE ANTI-JOIN IS WRITTEN AS `NOT IN`, NOT AS `NOT EXISTS`, AND IT IS A
-- MEASUREMENT RATHER THAN A STYLE. Under RLS the correlated NOT EXISTS cannot be
-- turned into an index probe: the policy wraps the subquery in a Result node with
-- a One-Time Filter, so the planner materialises the tenant's whole review index
-- and re-scans it once per candidate row -- a nested loop anti join. Measured on
-- the seed tenant (21 419 rows, 4 634 of them flagged), warm cache, after ANALYZE,
-- with EVERY flag already reviewed (which is the STEADY STATE of a panel a manager
-- keeps clear, i.e. the case that matters most):
--
--	NOT EXISTS, r.tenant_id = t.tenant_id       1356 / 1375 / 1506 ms
--	NOT EXISTS, r.tenant_id = <the parameter>    979 / 1047 ms
--	LEFT JOIN ... WHERE r.id IS NULL              70 / 100 ms
--	NOT IN  (this file)                           17.8 / 20.3 ms
--
-- The first two produce "Rows Removed by Join Filter: 10 734 661" for a query that
-- returns nothing. NOT IN is planned as a HASHED SubPlan: the tenant's reviewed ids
-- are collected once and each candidate is one hash probe.
--
-- ⚠️ NOT IN IS ONLY SAFE BECAUSE THE COLUMN IS NOT NULL. `x NOT IN (set)` is
-- UNKNOWN -- and therefore false -- if the set contains a NULL, which would empty
-- the queue silently. transaction_reviews.transaction_id is NOT NULL (00005), so
-- the set cannot contain one. If that ever changes, this shape breaks QUIETLY and
-- must change with it.
--
-- ⚠️ AND THE HASH IS BOUNDED BY work_mem, WHICH IS THE HONEST LIMIT. If a tenant's
-- reviewed set outgrows it the planner drops back to a non-hashed SubPlan and the
-- cost returns to the numbers in the first two rows above. The set grows with every
-- decision and section 4.3 makes those rows permanent, so this is a real ceiling
-- rather than a theoretical one. Bounding it properly is an index question (a
-- partial index on the pending set is not expressible, because "pending" spans two
-- tables) or a schema question, i.e. a migration -- deliberately not this task's.

-- name: CountPendingFlagged :one
-- How many flagged records are still waiting, COUNTED UP TO A CAP.
--
-- 🔴 IT IS CAPPED BECAUSE AN UNCAPPED COUNT IS A SEQUENTIAL SCAN. There is no index
-- on (tenant_id, verdict), so `count(*)` over the tenant's flagged rows is a
-- parallel seq scan of the whole table: measured 59.4 / 63.6 / 66.4 ms warm, against
-- 0.7 / 0.8 / 1.1 ms for the shape below. The ORDER BY is what makes the difference
-- -- it lets the planner walk transactions_tenant_occurred_idx (tenant_id,
-- occurred_at DESC) and stop as soon as the cap is full, so this query never reads
-- more of the timeline than it has to.
--
-- WHAT THE CAP COSTS, STATED: past it the number is a floor rather than a total, so
-- the caller renders "100+" and never a wrong exact figure. What it does NOT do is
-- hide a record -- the list below pages through every one of them.
SELECT count(*)::int AS pending
FROM (
    SELECT 1
    FROM transactions t
    WHERE t.tenant_id = @tenant_id
      AND t.verdict = 'flag'
      AND t.id NOT IN (
          SELECT r.transaction_id
          FROM transaction_reviews r
          WHERE r.tenant_id = @tenant_id
      )
    ORDER BY t.occurred_at DESC, t.id DESC
    LIMIT sqlc.arg('cap')::int
) AS capped;

-- name: ListFlaggedForReview :many
-- One page of the queue, newest first.
--
-- THE COLUMN LIST IS ListPanelTransactions' LIST, and the reason is section 4.7:
-- gps_lat, gps_lng, source_ip and policy_context are absent here for exactly the
-- argument that query states at length -- a column that is never selected cannot
-- reach a template, an attribute, a log line or a CSV. The screen shows the SIGNS
-- (ip_match, gps_match), which is what section 5 rows 6-7 decided on and is
-- precisely the evidence a manager is being asked to judge.
--
-- KEYSET PAGINATION on (occurred_at, id), the same as the transactions list and for
-- the same reason: transactions is append-only and new rows sort to the top, so an
-- OFFSET page would repeat rows. It is additionally right here because the queue
-- SHRINKS as decisions are made -- an OFFSET would skip records every time.
--
-- TENANT SCOPE (section 4.5, belt + braces): the explicit t.tenant_id predicate is
-- required even though RLS is on, every JOIN re-states tenant_id in its own ON
-- clause, and the anti-join subquery carries its own. The tenant comes from the
-- panel session, never from the request.
-- policy_layer travels with the note for the reason ListPanelTransactions gives at
-- length (M8-04 FAZ B3): the note names a rule, and the rule may sit on the
-- customer's own policy document. This queue is where it matters most -- every row
-- here is a `flag` awaiting a human, and whether the rule that flagged it is one
-- the reader can change is part of deciding. The column is a CHECK-constrained
-- three-word enum, so it is section 4.7-safe; policy_context still is not selected.
SELECT t.id, t.occurred_at, t.type, t.trust, t.verdict, t.channel, t.practice,
       t.queued, t.tag_uid, t.ctr, t.ip_match, t.gps_match, t.note, t.policy_layer,
       l.name AS location_name,
       d.name AS department_name,
       e.full_name AS employee_name
FROM transactions t
LEFT JOIN locations   l ON l.tenant_id = t.tenant_id AND l.id = t.location_id
LEFT JOIN departments d ON d.tenant_id = t.tenant_id AND d.id = t.department_id
LEFT JOIN employees   e ON e.tenant_id = t.tenant_id AND e.id = t.employee_id
WHERE t.tenant_id = @tenant_id
  AND t.verdict = 'flag'
  AND t.id NOT IN (
      SELECT r.transaction_id
      FROM transaction_reviews r
      WHERE r.tenant_id = @tenant_id
  )
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (t.occurred_at, t.id) < (sqlc.narg('cursor_at')::timestamptz,
                                   sqlc.narg('cursor_id')::uuid))
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT sqlc.arg('page_size')::int;

-- name: GetTransactionReview :one
-- WHO decided this record, and how. Read ONLY after a unique violation, to answer
-- the one question the refusal cannot answer on its own: was it you?
--
-- 🔴 IT EXISTS BECAUSE THE SCREEN WAS SAYING SOMETHING IT HAD NOT MEASURED. Without
-- it, a manager who double-clicks Approve gets "somebody else decided that record
-- first" for a decision they made themselves seconds earlier -- a sentence whose
-- SUBJECT is fabricated. That is the class M5-11 closed, and the diff's own
-- concurrency test produced the behaviour.
--
-- IT RETURNS THE REVIEWER AND THE OUTCOME AND NOTHING ELSE. The note is not
-- selected: it is a manager's free text about a named person, the caller only needs
-- to know who and what, and a column that is never selected cannot reach a screen.
--
-- TENANT SCOPE (section 4.5, belt + braces): explicit predicate on top of RLS. A
-- foreign transaction id simply matches nothing here, so this cannot become an
-- oracle for whether an id exists in another business.
SELECT r.reviewer_id, r.outcome
FROM transaction_reviews r
WHERE r.tenant_id = @tenant_id
  AND r.transaction_id = sqlc.arg('transaction_id')::uuid;

-- name: InsertTransactionReview :one
-- THE decision. One row, append-only, and it is the ONLY write this flow makes to
-- the record besides audit_log.
--
-- 🔴 IT IS AN INSERT ... SELECT RATHER THAN AN INSERT ... VALUES, and the SELECT is
-- the point: the row can only come into existence if a transaction in THIS tenant
-- with verdict='flag' exists to build it from. So three refusals are one predicate:
--
--	the id belongs to another tenant   -> 0 rows
--	the id does not exist              -> 0 rows
--	the record is ok/reject/ignored    -> 0 rows
--
-- and the caller sees pgx.ErrNoRows, which it turns into an honest sentence rather
-- than a 500. WITHOUT the verdict predicate this flow would be an arbitrary
-- RELABELLING CHANNEL: a manager could post the id of a trust-100 'ok' record and
-- have it recorded as rejected, which erases hours while section 4.3 looks intact.
--
-- 🔴 IT IS NOT THE ONLY DEFENCE AND MUST NOT BE READ AS ONE. Migration 00005's
-- tappa_check_transaction_review trigger enforces the same three rules plus
-- reviewer_id <> employee_id, and the composite FK (transaction_id, tenant_id) ->
-- transactions makes a cross-tenant review structurally impossible. This predicate
-- exists so the REFUSAL IS A SENTENCE instead of an exception; the trigger and the
-- FK are what make it a guarantee.
--
-- @tenant_id APPEARS TWICE ON PURPOSE: once as the column being written (which the
-- RLS WITH CHECK re-verifies against the transaction context) and once as the
-- SELECT's own scope predicate. They are the same value and neither is redundant --
-- the first says which tenant owns the review, the second says which tenant's
-- records may be read to build it.
--
-- UNIQUE (transaction_id) means a second decision on the same record raises 23505.
-- That is deliberate (00005: "a record is decided ONCE") and the caller must
-- translate it, never swallow it: silently treating it as success would tell the
-- loser of a race that their decision was recorded when somebody else's was
-- (section 4.6).
INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome, note)
SELECT @tenant_id, t.id, @reviewer_id, @outcome, sqlc.narg('note')::text
FROM transactions t
WHERE t.tenant_id = @tenant_id
  AND t.id = sqlc.arg('transaction_id')::uuid
  AND t.verdict = 'flag'
RETURNING id, reviewed_at;
