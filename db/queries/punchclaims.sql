-- punchclaims.sql -- an employee's claim of the missing half of a punch (M15-15, ADR 0032 §C.2-§C.3;
-- migration 00068). The window, the K4 rule, the lock and the decision are M15-16's
-- (internal/domain/claim); the schema card wrote the claim's INSERT, BUILT FROM THE ANCHOR ROW, and
-- M15-16 adds the domain's reads (the anchor, the chain around it, the claimed anchors, the claim
-- with its decision) and the decision's INSERT, built from the claim row the same way.

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

-- name: GetPunchClaimAnchor :one
-- ONE OF THE PERSON'S RECORDS, AS A CLAIM READS IT (M15-16, internal/domain/claim): the anchor a
-- claim names (Submit, Decide, Inspect) or the tap whose result screen may ask one (Offer). Under its
-- tenant AND its person -- the explicit predicate beside RLS (§4.5's belt) -- so another tenant's or
-- another person's id is ZERO ROWS, the same refusal InsertPunchClaim gives.
--
-- WHAT IT RETURNS IS THE ROW'S CHAIN FACTS AND ITS SHIFT, NOTHING THE TAP WAS JUDGED ON. type (NULL
-- for a refusal or an ignored tap -- no missing half, no card), occurred_at, business_day_start (NULL
-- before M15 and on a manual row -- ADR 0032 §C.1 Ü-Y2: no claim), practice, the venue the record is
-- filed at (location_id, or a holding peer's site_location_id -- ADR 0032 §C.4: the approval's manual
-- row goes to the anchor's venue), and the anchor's RESOLVED SHIFT: the department's when the row's
-- department has one, otherwise the venue's (§5, the rule tap.Decide's lateness reads) -- the
-- prefill's real end and the band (ADR 0032 §C.2, §C.4). The department is the ROW's (frozen at the
-- tap), the shift times today's. A row at a peer's venue has no local location row, so its shift is
-- the department's or none: no prefill, and the decision asks the confirmation step (ADR 0032 §C.4:
-- "vardiya yoksa"). No address, no coordinate, no verdict, no note.
--
-- M15-18: channel and direction_source are the anchor's mark on the panel's claim queue ("the person
-- chose this direction") -- read ONLY through tap.RecordedDirectionSource (ADR 0032 §B.4's one reader
-- rule), never compared raw: a manual row's value is not believed, a directed nfc/qr NULL is "auto".
SELECT a.id,
       a.type,
       a.channel,
       a.direction_source,
       a.occurred_at,
       a.created_at,
       a.business_day_start,
       a.practice,
       a.location_id,
       a.site_location_id,
       (CASE WHEN d.shift_start IS NOT NULL THEN d.shift_start ELSE l.shift_start END)::time AS shift_start,
       (CASE WHEN d.shift_start IS NOT NULL THEN d.shift_end ELSE l.shift_end END)::time     AS shift_end,
       COALESCE(CASE WHEN d.shift_start IS NOT NULL THEN d.overnight ELSE l.overnight END, false)::boolean
                                                                                       AS shift_overnight
  FROM transactions AS a
  LEFT JOIN departments AS d
         ON d.id = a.department_id
        AND d.tenant_id = @tenant_id
  LEFT JOIN locations AS l
         ON l.id = a.location_id
        AND l.tenant_id = @tenant_id
 WHERE a.id = @id
   AND a.tenant_id = @tenant_id
   AND a.employee_id = @employee_id::uuid;

-- name: ListPunchClaimChain :many
-- THE PERSON'S DIRECTION CHAIN AROUND A CLAIM (M15-16; ADR 0032 §A.6, §C.1, §C.3): every directed,
-- non-practice record with occurred_at in [@from_at, @to_at], in (occurred_at, id) order, AND the one
-- record immediately before @from_at (before_range true) -- the predecessor of the first record in
-- range, so that record's class (its P) is known. internal/domain/claim classifies these rows with
-- tap.ClassifyCheckout, the Go half of tappa_checkout_class (the parity test binds the two), so an
-- anchor's "still open / still an orphan", the window's ends and K4's two showings are the READER's
-- answer, never Decision.LeftOpen or Decision.MissingIn (the M15-10/M15-16 handover).
--
-- created_at is selected because the classification's W reads it (the database's write instant --
-- TestTransactions_NoStatementWritesCreatedAt). Nothing read here is evidence: direction, two
-- instants, the frozen business day and the channel.
--
-- BOUNDED BY TIME AND BY @row_limit: the caller asks for a window of hours (internal/domain/claim:
-- the anchor's day and 18 hours on either side) and REFUSES when the limit is reached, rather than
-- judging a truncated chain.
SELECT p.id, p.type, p.occurred_at, p.created_at, p.business_day_start, p.channel, p.before_range
  FROM (
        (SELECT b.id, b.type, b.occurred_at, b.created_at, b.business_day_start, b.channel,
                true AS before_range
           FROM transactions AS b
          WHERE b.tenant_id = @tenant_id
            AND b.employee_id = @employee_id::uuid
            AND b.type IS NOT NULL
            AND NOT b.practice
            AND b.occurred_at < @from_at
          ORDER BY b.occurred_at DESC, b.id DESC
          LIMIT 1)
        UNION ALL
        (SELECT r.id, r.type, r.occurred_at, r.created_at, r.business_day_start, r.channel,
                false AS before_range
           FROM transactions AS r
          WHERE r.tenant_id = @tenant_id
            AND r.employee_id = @employee_id::uuid
            AND r.type IS NOT NULL
            AND NOT r.practice
            AND r.occurred_at >= @from_at
            AND r.occurred_at <= @to_at
          ORDER BY r.occurred_at, r.id
          LIMIT @row_limit)
       ) AS p
 ORDER BY p.occurred_at, p.id;

-- name: ListPunchClaimedAnchors :many
-- WHICH OF THESE RECORDS ALREADY CARRY A CLAIM (M15-16, K4: "talep gönderildiyse ikinci gösterim
-- yok"; ADR 0032 §C.5: one claim per anchor, ever -- a rejected one included). Explicit tenant and
-- person beside RLS.
SELECT c.anchor_transaction_id
  FROM punch_claims AS c
 WHERE c.tenant_id = @tenant_id
   AND c.employee_id = @employee_id::uuid
   AND c.anchor_transaction_id = ANY (@anchor_ids::uuid[]);

-- name: GetPunchClaimForDecision :one
-- A CLAIM AND WHETHER IT IS DECIDED (M15-16, Decide and Inspect). Read once before the person's lock
-- (the claim names the person whose lock it is -- a claim row never changes, §4.3) and AGAIN under
-- it, where decided_outcome is the "already decided" answer (ADR 0009: a decision is final; the
-- UNIQUE punch_claim_decisions_one_per_claim is the braces). Explicit tenant on both tables.
SELECT c.id,
       c.employee_id,
       c.anchor_transaction_id,
       c.kind,
       c.claimed_at,
       c.prefilled_at,
       c.created_at,
       d.outcome AS decided_outcome
  FROM punch_claims AS c
  LEFT JOIN punch_claim_decisions AS d
         ON d.claim_id = c.id
        AND d.tenant_id = @tenant_id
 WHERE c.id = @id
   AND c.tenant_id = @tenant_id;

-- name: InsertPunchClaimDecision :one
-- THE MANAGER'S DECISION ON A CLAIM (M15-16; ADR 0032 §C.2, §C.4, §C.5; migration 00068). Built FROM
-- the claim row under its tenant (§4.5's belt): another tenant's claim id is ZERO ROWS -- pgx.ErrNoRows,
-- nothing written. An approval names the manual row it wrote and that row's moment (approved_at =
-- the row's occurred_at, which tappa_check_punch_claim_decision holds, with the row's person, channel,
-- direction, entered_by and write instant); a rejection names neither (the shape CHECK). A second
-- decision on one claim is 23505 (punch_claim_decisions_one_per_claim) -- "already decided"; one
-- manual row answering two claims is 23505 too (one_per_record). The note is the manager's free text:
-- stored here once, never logged and never copied into audit_log.
INSERT INTO punch_claim_decisions (tenant_id, claim_id, outcome, decided_by, approved_at, transaction_id, note)
SELECT c.tenant_id, c.id, @outcome::text, @decided_by::uuid,
       sqlc.narg('approved_at')::timestamptz, sqlc.narg('transaction_id')::uuid, sqlc.narg('note')::text
  FROM punch_claims AS c
 WHERE c.id = @claim_id
   AND c.tenant_id = @tenant_id
RETURNING id, created_at;

-- name: ListPendingPunchClaims :many
-- THE PANEL'S CLAIM QUEUE (M15-18; ADR 0032 §C.4): the claims of this business nobody has decided
-- yet, OLDEST FIRST -- (created_at, id), the order a manager works a backlog in, so a shortened list
-- keeps the claims that have waited longest. A claim whose anchor has since been closed another
-- way, or whose moment has aged past manual.MaxBackdate, is STILL here: it is undecided, and the
-- focus says "already resolved" / "expired" and offers Reject (§C.5) -- this list does not judge.
--
-- What crosses the boundary is ids, the person's name, the kind, three instants (the claimed moment,
-- when it was sent, the anchor's moment) and nothing the anchor was judged on. Explicit tenant on
-- every table it reads (§4.5's belt beside RLS). Bounded by @row_limit (the caller reads one past
-- its cap to say "more").
SELECT c.id,
       c.employee_id,
       e.full_name     AS employee_name,
       c.kind,
       c.claimed_at,
       c.created_at,
       a.occurred_at   AS anchor_at
  FROM punch_claims AS c
  JOIN transactions AS a
    ON a.id = c.anchor_transaction_id
   AND a.tenant_id = @tenant_id
  LEFT JOIN employees AS e
         ON e.id = c.employee_id
        AND e.tenant_id = @tenant_id
 WHERE c.tenant_id = @tenant_id
   AND NOT EXISTS (
       SELECT 1
         FROM punch_claim_decisions AS d
        WHERE d.tenant_id = @tenant_id
          AND d.claim_id = c.id
   )
 ORDER BY c.created_at, c.id
 LIMIT @row_limit;

-- name: ListPendingPunchClaimAnchors :many
-- WHICH OF THESE RECORDS CARRY A CLAIM NOBODY HAS DECIDED (M15-18): the "claim pending" mark on
-- Odd shapes' two lists (check-ins with no checkout, check-in missing). The lists name their rows
-- by transactions.id and this joins them to punch_claims.anchor_transaction_id -- an id, never a
-- timestamp (two records can share an instant; ADR 0032 §A.6 orders by (occurred_at, id) for that
-- reason). A rejected claim's anchor is NOT marked: the record still misses its half and nobody is
-- asked about it any more. Explicit tenant on both tables.
SELECT c.anchor_transaction_id
  FROM punch_claims AS c
 WHERE c.tenant_id = @tenant_id
   AND c.anchor_transaction_id = ANY (@anchor_ids::uuid[])
   AND NOT EXISTS (
       SELECT 1
         FROM punch_claim_decisions AS d
        WHERE d.tenant_id = @tenant_id
          AND d.claim_id = c.id
   );
