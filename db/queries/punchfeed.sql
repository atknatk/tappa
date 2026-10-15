-- punchfeed.sql -- M11 API-7, GET /v1/punches: the read end of the punch feed (ADR 0028 §4, §5, §8).
--
-- A page is TWO statements in one transaction of the key's tenant (WithTenant + the explicit
-- tenant_id filter of every statement -- CLAUDE.md §4.5), and THE ORDER IS THE PROOF:
--
--   1. GetPunchFeedHead reads the tenant's counter (last_seq = S) and the first event after the
--      cursor. §5: a tenant's counter row is locked from the increment to the commit, so the events
--      a snapshot sees are exactly 1..S -- the committed counter IS the last committed event, and
--      every event at or below S committed before this statement began.
--   2. ListPunchEvents reads (after, upto] with upto <= S, in seq order. Its snapshot is LATER than
--      the head's, so every event at or below S is in it: a page that comes back short has read
--      the whole window, and the window's end is a cursor that skips nothing.
--
-- Reading the head AFTER the page would let the counter run past what the page saw (a skipped
-- event); reading the page without its upper bound would return an event above the cursor it hands
-- out (a repeated one). Both are measured as mutants (ADR 0028, API-7 note).
--
-- No statement here names transactions: punch_events carries no evidence column (no address, GPS,
-- note, policy context or verdict -- 00048), so TestEvidenceReaders_EveryOneIsClassified has nothing
-- to file.

-- name: GetPunchFeedHead :one
-- The tenant's counter and the first event after the cursor (0 when there is none). One row, or
-- none when the tenant has no counter row -- which §5 says never happens (00048 opens one for every
-- tenant, the tenants trigger for a new one); the caller treats it as an outage, not as "empty".
SELECT fc.last_seq,
       coalesce((SELECT min(pe.seq)
                   FROM punch_events pe
                  WHERE pe.tenant_id = @tenant_id
                    AND pe.seq > @after_seq::bigint), 0)::bigint AS next_seq
FROM feed_counters fc
WHERE fc.tenant_id = @tenant_id;

-- name: ListPunchEvents :many
-- The events of one scope in (after_seq, upto_seq], in seq order, at most page_size of them -- the
-- caller asks for one more than it hands out, to know whether the window holds more. The scope is
-- a role the row carries (ADR 0028 §2: employer -- the punch's employee is this business's; location
-- -- the plaque is this business's); a single-tenant punch carries both. The range is a walk of
-- punch_events_pkey (tenant_id, seq); the role is a filter on it, bounded by the window.
SELECT pe.seq, pe.punch_id, pe.version, pe.status, pe.punch_time, pe.recorded_at, pe.channel, pe.direction,
       pe.employee_id, pe.employee_ref, pe.employee_code, pe.employee_company,
       pe.location_id, pe.location_ref, pe.location_name, pe.location_company, pe.tag_uid
FROM punch_events pe
WHERE pe.tenant_id = @tenant_id
  AND pe.seq > @after_seq::bigint
  AND pe.seq <= @upto_seq::bigint
  AND @role::text = ANY (pe.roles)
ORDER BY pe.seq
LIMIT @page_size;
