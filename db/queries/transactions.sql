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
-- i.e. the most recent NON-PRACTICE type='in' with no later type='out'. If a row is
-- found the next tap is 'out'; if none (pgx.ErrNoRows) the next tap is 'in'.
-- Ordering is by occurred_at, NOT by calendar day, so the overnight shift (Rusty
-- Bar 18:00-02:00) toggles correctly at a 02:00 exit. Neutral on verdict: 'in' only
-- ever appears on decided taps (reject/ignored carry NULL type), so filtering on the
-- direction column is enough; whether a flagged 'in' counts is a domain (M3) call,
-- not the store's. Uses the (tenant_id, employee_id, occurred_at DESC) index.
--
-- 🔴 `NOT t.practice` IS THE FIX FOR A MEASURED SECTION 5 VIOLATION (ADR 0008,
-- M5-11). This query used to be practice-NEUTRAL and the exclusion lived in the
-- caller, which discarded a practice row WITHOUT LOOKING AT THE ONE BENEATH IT. So
-- a practice row that merely sorted newest made a real, still-open check-in
-- invisible: the next tap resolved to 'in' instead of 'out', the real entry never
-- closed, and nothing signalled it (verdict ok, no note, no flag). Measured, three
-- rows per arm, the only difference being the practice row's occurred_at:
--
--      practice OLDER than the real 'in'  -> third tap 'out', 0 open check-ins
--      practice NEWER than the real 'in'  -> third tap 'in',  2 open check-ins
--
-- Reachable over plain HTTP: occurred_at is a shipped form field and the
-- sys:occurred-at-bound ceiling is 72 h. It is also exactly the shape M9-01's
-- offline queue will produce.
--
-- WHY IN THE QUERY AND NOT IN GO. "The person's last open check-in" is ONE
-- question, and answering it needs the ordering -- a caller that reads a single row
-- and rejects it cannot see past it without asking again. Deciding it here also
-- makes this query agree with the anomaly count in
-- internal/handler/seedflow_db_test.go (openCheckIns), which already carried
-- `AND NOT t.practice`.
--
-- SCOPE, exactly: the predicate is on the OUTER row only. The NOT EXISTS stays
-- practice-neutral because it asks a different question -- "was this entry closed
-- later" -- and a closing 'out' closes it whatever flag it carries. It is also moot
-- today: a practice row is ALWAYS type='in' (tap.isPracticeTap requires no prior
-- tap and no open check-in, so resolveDirection cannot return 'out' for it), pinned
-- by TestDecide_PracticeIsAlwaysAnIn.
--
-- COST, MEASURED (EXPLAIN (ANALYZE, BUFFERS), 5001 rows for one person, ADR 0008):
-- the predicate NEVER narrows the index range -- `practice` is not in
-- transactions_tenant_employee_occurred_idx, so it lands as
-- `Filter: ((NOT practice) AND (type = 'in'))` next to the type filter that was
-- always there. What it changes is where LIMIT 1 stops. Person currently checked
-- IN (the defect shape): 7 -> 9 buffers. Person currently checked OUT with a
-- practice row on top: 9 -> 16090 buffers, because the practice row was buying an
-- early exit by returning the WRONG row; the same person WITHOUT a practice row
-- already costs 10246 buffers today. So this is not a new worst case, it is the
-- one every ordinary employee's check-in already pays. Making it cheap is an
-- INDEX question (a migration, deliberately out of M5-11's scope).
SELECT id, tenant_id, employee_id, location_id, department_id, tag_uid, ctr,
       type, occurred_at, source_ip, ip_match, gps_lat, gps_lng, gps_match,
       sun_valid, trust, verdict, note, channel, entered_by, practice, queued,
       created_at, policy_version_id, matched_sid, policy_layer, policy_context
FROM transactions t
WHERE t.tenant_id = @tenant_id
  AND t.employee_id = @employee_id
  AND t.type = 'in'
  AND NOT t.practice
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

-- name: LockEmployeeForTap :exec
-- SERIALISE ONE PERSON'S TAP DECISION (ADR 0006, layer 4).
--
-- 🔴 THE DEBOUNCE IS A READ-THEN-DECIDE, and without this it loses the same way
-- CLAUDE.md section 4.4 says a counter check loses: N concurrent requests all
-- gather BEFORE any of them has written, so every one of them sees the same old
-- predecessor and none of them is a duplicate. Measured, -race, one employee:
-- 50 simultaneous POSTs -> 51 counted rows, zero ignored, in 0.41 s. TWO
-- simultaneous POSTs are already enough.
--
-- The lock is TRANSACTION-SCOPED (pg_advisory_xact_lock), so it is released by
-- COMMIT or ROLLBACK and no code path can leak it. It is taken as the FIRST
-- statement of the transaction that gathers, decides and writes, so the whole
-- read-decide-write is serial FOR THIS PERSON.
--
-- THE KEY IS 64 BITS DERIVED FROM (tenant_id, employee_id). Advisory locks live
-- in one cluster-wide space and are NOT scoped by RLS or by database role, so the
-- tenant MUST be in the key or two tenants could collide by employee id alone.
-- A hash collision serialises two unrelated people. It can never mix their data
-- -- every statement inside still carries its own tenant_id filter and runs under
-- RLS -- but the WAIT IS NOT SMALL, and calling it "a few milliseconds" was the
-- fifth absolute sentence this change had measured out from under it. The loser
-- waits for the WHOLE locked section: measured, holding a person's key from
-- outside for 3 s delayed their POST by 3.32 s, and 50 simultaneous taps by one
-- person put the worst request at 1.46 s (QR) / 1.91 s (NFC). The ceiling is
-- middleware.Timeout(30s); this repo sets no statement_timeout or lock_timeout.
--
-- DIFFERENT PEOPLE DO NOT WAIT ON EACH OTHER'S LOCK: distinct keys, no shared row
-- lock.
--
-- 🔴 THEY DO STILL PAY FOR IT, THOUGH, and an earlier version of this comment said
-- otherwise. A waiter HOLDS ITS POOL CONNECTION while waiting, so a flood aimed at
-- ONE key parks connections that are doing nothing: sampled pg_stat_activity showed
-- up to 15 of 16 pooled connections in wait_event='advisory', versus 0 for the same
-- row volume spread across separate keys. Clean A/B (flood 150, victim a SINGLE
-- shot at a fixed 200 ms offset, fresh session per round, 3 rounds): an uninvolved
-- third party's single tap took 1.60/1.05/1.14 s against one key and
-- 0.178/0.178/0.176 s against separate keys -- 6-9x worse. Ceilings: the ByAddress
-- budget (3000/10min) and middleware.Timeout(30s). No record is lost either way.
--
-- ⚠️ MEASURING THIS WRONG IS EASY: a one-key flood driven from a SINGLE session
-- mostly returns 429 from the BySession 300/10min limit without ever reaching the
-- lock (a 200-request flood finished in 40 ms, all rate limited), which reads as
-- "no difference". Use distinct sessions for the flood and one shot for the victim.
SELECT pg_advisory_xact_lock(hashtextextended((sqlc.arg(tenant_id)::uuid)::text || ':' || (sqlc.arg(employee_id)::uuid)::text, 0));

-- name: SecondsSinceLastRecordedTap :one
-- THE SERVER-CLOCK LEG OF THE DEBOUNCE (ADR 0006): when did this person's most
-- recent TAP get WRITTEN? Not "claim to have happened" -- written.
--
-- 🔴 WHY THIS IS A SEPARATE QUERY AND NOT A COLUMN OFF THE ROW BELOW. The row
-- below is chosen by ORDER BY occurred_at, which is a CLIENT-DECLARABLE column,
-- so a caller can decide WHICH row is treated as the predecessor: declare a time
-- under the person's existing newest row and every new row sorts beneath it, so
-- the predecessor never advances and both legs of the gap keep measuring one
-- untouched old record (measured: 20 posts, 20 counted rows, 0.31 s). Ordering by
-- created_at instead answers the question the debounce actually asks -- "does
-- this person have a tap RECORDED in the last N seconds" -- and no caller can
-- reorder it, because created_at is the database's own now().
--
-- channel IN ('nfc','qr') IS THE MANUAL EXEMPTION, expressed where it cannot be
-- dodged. created_at on a manual row is when a MANAGER TYPED IT, which carries no
-- claim about where the employee was: counting it would debounce an employee's
-- genuine tap thirty seconds after a manager's backdated entry, and would break
-- bulk manual entry. channel is server-derived, so this predicate is not a lever
-- a caller can reach.
--
-- 🔴 IT RETURNS AN AGE, NOT A TIMESTAMP, AND THAT IS LOAD-BEARING. An earlier
-- version returned created_at and let Go subtract it from its own clock. That
-- mixes two clock domains, and under contention it INVERTS: the application
-- captures `now` before waiting on the lock, while created_at is a LATER
-- transaction's start time, so the subtraction goes NEGATIVE and the skew guard
-- discards the leg -- which is exactly how a serialised request still came back
-- `ok`. Measured: now=…46.400 against a predecessor created_at=…46.475, 75 ms in
-- the future. Computing the age HERE keeps both ends on the database clock, so
-- there is no skew to guard against and no dependence on separate hosts agreeing.
--
-- clock_timestamp(), NOT now(): now() is the TRANSACTION START time, which in a
-- request that waited on the lock predates the row it is measuring against.
--
-- 🔴 IT IS BOUNDED BY THE DEBOUNCE WINDOW, and this runs INSIDE the per-person
-- advisory lock, so its cost is paid while holding a pool connection. Without the
-- bound it sorted the person's ENTIRE lifetime history: section 4.6 writes a row
-- for every flood POST and section 4.3 makes those rows permanent, so the scanned
-- set only ever grows, fastest for the very person being flooded.
--
-- The caller discards anything at or beyond the window anyway (the guardrail
-- fires on `gap < window`), so bounding here changes no decision — it only stops
-- the sort from ordering rows nobody can use. MEASURED on one employee with
-- 20 002 rows: sort input 20 002 -> 43, execution 22.1 / 47.5 / 11.8 ms ->
-- 10.2 / 10.2 / 15.8 ms.
--
-- ⚠️ IT BOUNDS THE SORT, NOT THE SCAN, and that is the honest limit. The index is
-- (tenant_id, employee_id, occurred_at); created_at is not in it, so the Bitmap
-- Heap Scan still visits every row of the person's history and throws them away
-- (measured: "Rows Removed by Filter: 19959"). Making the scan itself bounded
-- needs an index on created_at, which is a migration and was NOT taken here.
--
-- pgx.ErrNoRows means "no tap recorded for this person INSIDE THE WINDOW", which
-- never debounces (the safe zero value, section 4.6) — the same answer the
-- unbounded query produced by returning an age too large to win the min.
SELECT EXTRACT(EPOCH FROM (clock_timestamp() - created_at))::float8 AS seconds_since
FROM transactions
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND channel IN ('nfc', 'qr')
  AND created_at > clock_timestamp() - make_interval(secs => sqlc.arg(window_seconds)::float8)
ORDER BY created_at DESC
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

-- name: ListPanelTransactions :many
-- The PANEL's read of the day (M6-03). One page of the immutable record, newest
-- first, filtered by the six things a manager asks about: a day, a location, a
-- department, a person, a verdict and a channel.
--
-- 🔴 IT IS A READ AND ONLY A READ. transactions is IMMUTABLE (section 4.3); the
-- correction flow is M6-08 and it writes a NEW ROW plus audit_log, never here.
-- Filters HIDE rows, they never remove them -- a page that shows nothing has still
-- asked the database (section 4.6).
--
-- 🔴 SECTION 4.7 -- WHAT THIS QUERY DELIBERATELY DOES NOT SELECT, and the absence is
-- the point rather than an oversight. The row on disk carries gps_lat, gps_lng and
-- source_ip; NONE of the three is in the column list. The screen shows the SIGNS
-- (ip_match, gps_match) because that is what section 5 rows 6-7 actually decided
-- on, and a coordinate that is never selected is a coordinate that cannot reach a
-- template, an HTML attribute, a log line or a CSV. policy_context is left out for
-- the same reason at one remove: it is section 4.7-safe by construction (a
-- DISTANCE, migration 0008) but it is a decision snapshot for replay, not screen
-- content, and selecting it would put every future key it gains on a rendered page.
--
-- 🔴 AND NEITHER ARE entered_by, location_id, department_id OR employee_id, which
-- an audit found this query selecting and NOTHING reading. That is not tidiness:
-- the paragraph above defends this screen on the ground that a column which is
-- never SELECTED cannot reach a template, and four columns carried for no reader
-- weaken exactly that argument -- entered_by identifies an ADMINISTRATOR, and the
-- three ids are the joins' INPUTS rather than the panel's output. The names below
-- are what the screen shows; the ids stay in the database. Whoever needs one for a
-- link (M6-04's review queue will) adds it back WITH its reader, in one edit.
--
-- TENANT SCOPE (section 4.5, belt + braces): the explicit t.tenant_id predicate is
-- REQUIRED even though RLS is on, and every JOIN re-states tenant_id in its own ON
-- clause so a joined name can never come from a neighbouring tenant even if a
-- foreign id somehow reached the column. The tenant comes from the panel session
-- (httpx.AdminIdentity), never from the request.
--
-- THE JOINS ARE LEFT JOINS BECAUSE THE COLUMNS ARE NULLABLE, and migration 0005
-- explains why they have to be: a stolen plaque touched with no cookie writes a
-- reject with NO employee_id, and that is the record we most want to keep. An
-- INNER JOIN here would silently drop exactly those rows -- a section 4.6 breach
-- committed by a read.
--
-- KEYSET PAGINATION, NOT OFFSET. transactions is append-only and new rows sort to
-- the TOP of occurred_at DESC, so an OFFSET page 2 fetched after a fresh tap lands
-- would repeat a row page 1 already showed. The cursor is the (occurred_at, id)
-- pair of the last row rendered; the id breaks ties between taps sharing a
-- timestamp so no row can be skipped or shown twice. It is client-supplied and
-- that is safe: it can only move the reader within their OWN tenant's timeline,
-- because the tenant predicate above is not a parameter the client controls.
--
-- THE PERSON FILTER IS A NAME MATCH RATHER THAN AN ID (user decision, 2026-08-06),
-- and the reason is a measurement rather than a preference. The id form needed the
-- panel to render every employee of the tenant as an <option> so one could be
-- picked: measured on a real page, that list was 835 319 of the page's 867 233
-- bytes -- 96% -- and it grew with the payroll forever, on a page that is
-- Cache-Control: no-store and therefore re-sent on every view and every filter
-- change. Matching here instead makes the control a text box and the page ~31 KB,
-- whatever size the business is.
--
-- 🔴 IT MATCHES REGARDLESS OF STATUS, AND THAT IS §4.6 RATHER THAN AN OVERSIGHT.
-- The alternative shortlists (active only; anyone who tapped recently) were all
-- measured and REJECTED for exactly this: they make a person who has left
-- unfindable through the panel, and their records are the ones a manager most often
-- needs months later. There is no status predicate below and there must not be one.
--
-- 🔴 THE MATCH RUNS IN A MATERIALIZED CTE, AND THE NUMBER THAT FIRST JUSTIFIED IT
-- DID NOT REPRODUCE. This block is rewritten because an audit could not confirm it,
-- and re-measuring showed the original claim was an artefact.
--
-- WHAT WAS OBSERVED, AND IT WAS REAL. With the ILIKE as a filter over the LEFT
-- JOIN, EXPLAIN ANALYZE showed a NESTED LOOP re-scanning the tenant's employees
-- once per transaction row -- 318 loops, 107 319 rows removed by the join filter,
-- 343 443 buffer hits, 14.3 s for one page. That output is real; the CONCLUSION
-- drawn from it ("the join shape is 27x slower") was not.
--
-- WHY IT DID NOT REPRODUCE, MEASURED RATHER THAN GUESSED. The development database
-- had NEVER been analysed: pg_stat_user_tables showed last_analyze, last_autoanalyze
-- and last_autovacuum all NULL, with n_live_tup = 5 326 for a table holding 111 167
-- rows. The planner was choosing plans from statistics roughly twenty times too
-- small. After ANALYZE, on the same tenant and the same day, warm cache, 5 repeats:
--
--	JOIN filter (the original shape)   2.0 - 2.2 ms
--	CTE AS MATERIALIZED (this one)     7.6 - 10.0 ms
--	CTE AS NOT MATERIALIZED            7.7 - 17.7 ms
--
-- So on correct statistics the shape this replaced is about FOUR TIMES FASTER, and
-- MATERIALIZED versus NOT MATERIALIZED is indistinguishable. The "27x" and "95x"
-- figures are withdrawn.
--
-- 🔴 SO WHY KEEP IT. Not for speed -- it costs roughly 6-8 ms per filtered request
-- on this data, and the gap widens with how many names the pattern matches
-- (~6 ms for a selective one, ~37 ms for one matching many; and for a pattern
-- matching NOTHING the CTE is the FASTER of the two) -- but because the two shapes fail differently. The CTE's cost is
-- BOUNDED: employees is scanned once (verified, loops=1) and the outer query probes
-- a small id set, so the worst case is O(employees) + O(rows in the day). The join
-- filter's cost is PLANNER-DEPENDENT, and this repository has one measured instance
-- of that planner choosing O(rows in the day x employees) and taking 14 s -- on a
-- surface whose budget is 300 requests per session, sharing a connection pool with
-- the tap path. Paying 6-8 ms to remove a tail that was observed once is the trade;
-- it is written down so somebody who disagrees can reverse it knowingly.
--
-- ⚠️ LIMIT: NOTHING TESTS THE KEYWORD. Removing MATERIALIZED leaves the whole suite
-- green, because with current statistics the planner produces loops=1 either way --
-- so its absence is invisible to any behavioural test, and a test that grepped for
-- the word would be a change detector rather than a net. The guarantee here is an
-- argument about worst cases, not an asserted invariant.
--
-- ⚠️ LIMIT -- UNICODE CASE FOLDING, AND IT MATTERS FOR BOTH OUR MARKETS. ILIKE
-- folds case with the database collation, which is not the same as language-aware
-- folding. Measured against real rows:
--
--	'haddiem'  finds "Haddiem Zammit" (h-bar)          works
--	'ZAMMIT' / 'zammit' both find "Zammit" (z-dot)     works
--	'istanbul' finds "Istanbul Isik" (dotted I)        works
--	'ISIK'     does NOT find "Isik"                    FAILS  (lower('I') is 'i',
--	                                                   never dotless 'i')
--	NFC "Cafe" does NOT find an NFD-stored "Cafe"      FAILS  (Postgres does not
--	                                                   normalise; the accent is a
--	                                                   separate code point)
--
-- Malta and Turkey are the two markets, so the Turkish dotless-i case is a real
-- one rather than a curiosity. NO SECURITY CONSEQUENCE and no §4.6 consequence:
-- a name that fails to match narrows to nothing, and the UNFILTERED day still
-- lists every record -- so nothing becomes unreachable, it just is not found by
-- that spelling. Fixing it properly means a normalising/ICU collation or a
-- generated normalised column, i.e. a migration.
--
-- NO INDEX IS ADDED. A leading-wildcard ILIKE cannot use a btree, so going faster
-- means pg_trgm plus a GIN index -- an extension and a migration, which is not this
-- task's to write. The CTE's employee scan measured on its own is 6.5-8.6 ms for
-- 7 769 employees (EXPLAIN ANALYZE, warm cache, 3 repeats, after ANALYZE; an
-- independent audit measured 9.8-11.2 ms on the same shape). That does not justify
-- an extension plus a migration.
--
-- ⚠️ EVERY NUMBER IN THIS BLOCK CARRIES ITS CONDITIONS because the ones that did
-- not were withdrawn: they were taken on a database that had never been ANALYZEd,
-- and the planner was choosing from statistics ~20x too small.
--
-- Uses transactions_tenant_occurred_idx (tenant_id, occurred_at DESC), which
-- migration 0005 created for exactly this shape.
WITH matched_employees AS MATERIALIZED (
    -- Empty, cheaply, when no name was typed: the guard below is a one-time filter
    -- the planner evaluates before touching the table.
    SELECT e2.id
    FROM employees e2
    WHERE e2.tenant_id = @tenant_id
      AND sqlc.narg('employee_name')::text IS NOT NULL
      AND e2.full_name ILIKE '%' || sqlc.narg('employee_name')::text || '%'
)
SELECT t.id, t.occurred_at, t.type, t.trust, t.verdict, t.channel, t.practice,
       t.queued, t.tag_uid, t.ctr, t.ip_match, t.gps_match, t.note,
       l.name AS location_name,
       d.name AS department_name,
       e.full_name AS employee_name
FROM transactions t
LEFT JOIN locations   l ON l.tenant_id = t.tenant_id AND l.id = t.location_id
LEFT JOIN departments d ON d.tenant_id = t.tenant_id AND d.id = t.department_id
LEFT JOIN employees   e ON e.tenant_id = t.tenant_id AND e.id = t.employee_id
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at <  @to_at
  AND (sqlc.narg('location_id')::uuid IS NULL
       OR t.location_id = sqlc.narg('location_id')::uuid)
  AND (sqlc.narg('department_id')::uuid IS NULL
       OR t.department_id = sqlc.narg('department_id')::uuid)
  AND (sqlc.narg('employee_name')::text IS NULL
       OR t.employee_id IN (SELECT id FROM matched_employees))
  AND (sqlc.narg('verdict')::text IS NULL
       OR t.verdict = sqlc.narg('verdict')::text)
  AND (sqlc.narg('channel')::text IS NULL
       OR t.channel = sqlc.narg('channel')::text)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR (t.occurred_at, t.id) < (sqlc.narg('cursor_at')::timestamptz,
                                   sqlc.narg('cursor_id')::uuid))
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT sqlc.arg('page_size')::int;
