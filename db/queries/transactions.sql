-- transactions.sql -- tenant-scoped reads and the single append-only write for
-- the product's core record. Every query carries an explicit tenant_id filter
-- (CLAUDE.md section 4.5, belt + braces on RLS). transactions is IMMUTABLE
-- (section 4.3): no UPDATE/DELETE queries exist here by design -- corrections are
-- a new row + audit_log, and the FLAGGED review flow writes to
-- transaction_reviews, never here.
--
-- POLICY DECISION COLUMNS (M3-07, migration 0008): policy_version_id, matched_sid,
-- policy_layer and policy_context bind each record to WHY it got its verdict. They
-- are part of every column list below so this file keeps returning the full store
-- Transaction model (additive). The tap engine's caller (M5-05) SUPPLIES the values
-- from policy.Decision + a section 4.7-safe policy.Context snapshot (a GPS DISTANCE,
-- never a raw coordinate); the DB CHECK transactions_policy_decision_consistent
-- guarantees the four columns are mutually consistent or all absent.

-- name: GetLastOpenTransaction :one
-- Direction toggle basis (CLAUDE.md section 5): the person's last OPEN check-in,
-- i.e. the most recent type='in' with no later type='out'. If a row is found the
-- next tap is 'out'; if none (pgx.ErrNoRows) the next tap is 'in'. Ordering is by
-- occurred_at, NOT by calendar day, so the overnight shift (Rusty Bar 18:00-02:00)
-- toggles correctly at a 02:00 exit. Neutral on verdict: 'in' only ever appears on
-- decided taps (reject/ignored carry NULL type), so filtering on the direction
-- column is enough; whether a flagged 'in' counts is a domain (M3) call, not the
-- store's. Uses the (tenant_id, employee_id, occurred_at DESC) index.
SELECT id, tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
       type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
       sun_valid, trust, verdict, note, channel, entered_by, practice, queued,
       created_at, policy_version_id, matched_sid, policy_layer, policy_context
FROM transactions t
WHERE t.tenant_id = @tenant_id
  AND t.employee_id = @employee_id
  AND t.type = 'in'
  AND NOT EXISTS (
      SELECT 1
      FROM transactions o
      WHERE o.tenant_id = t.tenant_id
        AND o.employee_id = t.employee_id
        AND o.type = 'out'
        AND o.occurred_at > t.occurred_at
  )
ORDER BY t.occurred_at DESC
LIMIT 1;

-- name: GetLastTransactionForEmployee :one
-- Debounce basis (CLAUDE.md section 5, row 5): the person's single most recent
-- tap. Debounce is PER-PERSON, not per-tag, so a queue of different people can
-- tap the same wall plaque back to back. The domain compares this row's
-- occurred_at with the new tap's time against the 60s window.
SELECT id, tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
       type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
       sun_valid, trust, verdict, note, channel, entered_by, practice, queued,
       created_at, policy_version_id, matched_sid, policy_layer, policy_context
FROM transactions
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
ORDER BY occurred_at DESC
LIMIT 1;

-- name: InsertTransaction :one
-- THE single write path. id and created_at use DB defaults; every other column
-- is set by the tap engine, including practice and queued (no silent default is
-- relied on for those two -- the engine states them). tenant_id is provided
-- explicitly and WITH CHECK confirms it matches the context (section 4.5).
-- employee_id/location_id/department_id/tag_uid/ctr are nullable so a
-- context-less reject (stolen plaque, no session) is still recorded -- section
-- 4.6, a record is never lost.
--
-- The four POLICY-DECISION columns (M3-07) are also written here so the record
-- carries WHY it got its verdict: policy_version_id (the pinned append-only version,
-- NULL for a guardrail/default decision), matched_sid (the deciding sid --
-- MACHINE-FILTERABLE, "sys:..." | tenant sid | "default"), policy_layer
-- (guardrail|baseline|tenant) and policy_context (the section 4.7-safe input
-- snapshot). The caller passes them from policy.Decision; passing all four NULL is
-- valid too (the consistency CHECK permits the all-absent state, section 4.6).
INSERT INTO transactions (
    tenant_id, employee_id, location_id, department_id, tag_uid, ctr, type,
    occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match, sun_valid,
    trust, verdict, note, channel, entered_by, practice, queued,
    policy_version_id, matched_sid, policy_layer, policy_context
) VALUES (
    @tenant_id, @employee_id, @location_id, @department_id, @tag_uid, @ctr, @type,
    @occurred_at, @source_ip, @ip_match, @gps_lat, @gps_lng, @gps_match,
    @sun_valid, @trust, @verdict, @note, @channel, @entered_by, @practice, @queued,
    @policy_version_id, @matched_sid, @policy_layer, @policy_context
)
RETURNING id, tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
          type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
          sun_valid, trust, verdict, note, channel, entered_by, practice, queued,
          created_at, policy_version_id, matched_sid, policy_layer, policy_context;
