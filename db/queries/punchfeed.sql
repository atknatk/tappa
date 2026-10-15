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

-- THE HISTORY QUERY (M11 API-7b; ADR 0028 §5's last sentence, plan §2 item 18 and §5 API-7b):
-- GET /v1/punches?from=…&to=…, the events whose punch_time is in [from, to), in (punch_time, seq)
-- order, page by page. punch_time is the phone's declared instant (72 h back at most for an
-- offline queue, and an approved flag enters with its old punch_time days later), so an event can
-- be COMMITTED into a stretch of that order a reader has already passed. The guarantee is therefore
-- tied to a snapshot: the first page reads the counter S0 (GetPunchFeedCounter) BEFORE its window
-- and its page, the cursor carries S0, and every statement answers seq <= S0 only. Every event at or
-- below S0 committed before S0 was read (§5), so the set the reader walks is fixed and complete;
-- reading S0 after the page would let it cover an event committed between them into the stretch
-- the page had passed -- a skipped event (measured as a mutant, ADR 0028 API-7b note). An event
-- above S0 is the feed's (cursor=cur_…) to deliver, never the history's.
--
-- A page is three statements: the counter, the window's end (at most window positions of the
-- range after the cursor, any scope, any seq), the page (the scope's events at or below S0 up to
-- that end). The window bounds a page's walk whatever the scope's density and whatever the S0.

-- name: GetPunchFeedCounter :one
-- The tenant's counter: the history query's S0 on a first page, the bound a later page's S0 is held
-- to. No row means no counter row, which §5 says never happens (the caller treats it as an outage).
SELECT fc.last_seq
FROM feed_counters fc
WHERE fc.tenant_id = @tenant_id;

-- name: GetPunchHistoryWindow :one
-- How many positions of the range follow the cursor (counted to scan_size = window_size + 1, every scope
-- and EVERY seq -- one more than the window, to know whether the range goes on past it) and the last
-- of the first window_size of them; with none, the cursor's own position comes back.
--
-- 🔴 THE WINDOW COUNTS POSITIONS, NOT THE SNAPSHOT'S ROWS (API-7b review, G-ORTA). seq <= S0 is NOT
-- here: the walk is over (tenant_id, punch_time), so a seq filter is a heap read per row that no
-- LIMIT stops -- a stale or forged S0 made one page read every row of the range (measured: 1 M rows
-- removed by the filter, 230-660 ms a request). Counted without it, a page reads at most window + 1
-- positions whatever its S0; the snapshot's filter is ListPunchHistory's. The guarantee is untouched:
-- the keyset walks the whole order, and the set at or below S0 is fixed. The
-- range is a walk of punch_events_tenant_time_idx (tenant_id, punch_time); the redundant
-- punch_time >= after_time is the predicate that index can bound, the row comparison breaks ties
-- by seq.
WITH win AS (
    SELECT pe.punch_time, pe.seq
    FROM punch_events pe
    WHERE pe.tenant_id = @tenant_id
      AND pe.punch_time >= @from_time::timestamptz
      AND pe.punch_time < @to_time::timestamptz
      AND pe.punch_time >= @after_time::timestamptz
      AND (pe.punch_time, pe.seq) > (@after_time::timestamptz, @after_seq::bigint)
    ORDER BY pe.punch_time, pe.seq
    LIMIT @scan_size
), inside AS (
    SELECT w.punch_time, w.seq FROM win w ORDER BY w.punch_time, w.seq LIMIT @window_size
)
SELECT (SELECT count(*) FROM win)::bigint AS scanned,
       coalesce((SELECT i.punch_time FROM inside i ORDER BY i.punch_time DESC, i.seq DESC LIMIT 1), @after_time::timestamptz)::timestamptz AS end_time,
       coalesce((SELECT i.seq FROM inside i ORDER BY i.punch_time DESC, i.seq DESC LIMIT 1), @after_seq::bigint)::bigint AS end_seq;

-- name: ListPunchHistory :many
-- The scope's events of the range after the cursor and up to the window's end, seq <= S0, in
-- (punch_time, seq) order -- one more than the caller hands out. The same columns as ListPunchEvents.
SELECT pe.seq, pe.punch_id, pe.version, pe.status, pe.punch_time, pe.recorded_at, pe.channel, pe.direction,
       pe.employee_id, pe.employee_ref, pe.employee_code, pe.employee_company,
       pe.location_id, pe.location_ref, pe.location_name, pe.location_company, pe.tag_uid
FROM punch_events pe
WHERE pe.tenant_id = @tenant_id
  AND pe.seq <= @s0::bigint
  AND pe.punch_time >= @from_time::timestamptz
  AND pe.punch_time < @to_time::timestamptz
  AND pe.punch_time >= @after_time::timestamptz
  AND (pe.punch_time, pe.seq) > (@after_time::timestamptz, @after_seq::bigint)
  AND pe.punch_time <= @end_time::timestamptz
  AND (pe.punch_time, pe.seq) <= (@end_time::timestamptz, @end_seq::bigint)
  AND @role::text = ANY (pe.roles)
ORDER BY pe.punch_time, pe.seq
LIMIT @page_size;
