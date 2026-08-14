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
-- THE TAP write path -- and it stopped being THE ONLY one on 2026-08-10.
--
-- 🔴 THIS COMMENT SAID "THE single write path" UNTIL M6-08 ADDED THE SECOND ONE.
-- InsertManualTransaction (below) is a manager typing a record for somebody with no
-- phone, and it is a DIFFERENT STATEMENT rather than a second caller of this one --
-- see its own header for why the narrowing is done in SQL. Both remain INSERTs and
-- there is still no UPDATE or DELETE of `transactions` anywhere in db/queries
-- (section 4.3), which is the property the old sentence was reaching for; what the
-- sentence actually claimed was a count, and the count changed.
--
-- WHAT IS STILL SINGULAR: this is the only statement the TAP path writes, and the
-- only one that may set tag_uid, ctr, sun_valid, source_ip, ip_match, gps_lat,
-- gps_lng, gps_match or the four policy-decision columns. A manager-entered record
-- names none of them, and its statement has no parameter for any of them.
--
-- id and created_at use DB defaults; every other column
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

-- name: InsertManualTransaction :one
-- THE MANAGER'S OWN RECORD (M6-08). Somebody with no phone worked a shift, or an
-- entry was left open and nobody tapped out (Q18: the system produces NO checkout of
-- its own). A named administrator types it, and their id is on the row forever.
--
-- 🔴 WHY IT IS A SEPARATE STATEMENT AND NOT A SECOND CALLER OF InsertTransaction.
-- That one takes verdict, channel, type, practice, queued, sun_valid, tag_uid, ctr,
-- source_ip and the coordinates AS PARAMETERS, because the tap engine computes all of
-- them. This path computes none of them, and passing constants from Go would put the
-- narrowing in a Go file where the next edit can widen it silently. Here the narrowing
-- IS the statement:
--
--	verdict   literal 'ok'      this path cannot produce reject, flag or ignored
--	channel   literal 'manual'  and therefore cannot masquerade as a tap
--	practice  literal false     a manager's keystroke is nobody's training tap
--	queued    literal false     queued is the APPROVAL queue (00005: flag -> true)
--	tag_uid / ctr / sun_valid / source_ip / ip_match /
--	gps_lat / gps_lng / gps_match / the four policy columns
--	                            NOT IN THE COLUMN LIST AT ALL -> NULL
--
-- 🔴 THE ABSENT COLUMNS ARE THE SECTION 4.7 WALL AND THE SECTION 5 ONE AT THE SAME
-- TIME. A column this statement cannot name is a column this path cannot fill: there
-- is no parameter a coordinate could arrive in, and no parameter that could claim a
-- chip was read. sun_valid stays NULL rather than false because 00005 keeps three
-- states and the third is the true one here -- "not evaluated for this channel".
-- Writing false would say "we checked the chip and it failed" about a check that
-- never applied, and internal/domain/checkin's insertParams already refuses to write
-- it for the same reason.
--
-- 🔴 verdict = 'ok' IS A DECISION, NOT A DEFAULT, and it is the one thing here worth
-- arguing with. Section 4.6 says a tap with insufficient evidence is flagged, and a
-- manual record has NO machine evidence at all -- so `flag` looks like the careful
-- answer. It is not: Q18 says the record "always rests on a human's declaration", and
-- the declaration IS the evidence, carried by entered_by, which names somebody
-- answerable. A flagged manual record would sit in the approval queue forever (the
-- person who would approve it is the person who wrote it), its hours would stay out
-- of the payroll total (internal/domain/ledger's endpointState), and the manager
-- closing a forgotten checkout would find the total still short -- which is the exact
-- failure Q18 exists to prevent. What makes the row honest is not a worse verdict but
-- the CHANNEL, which every report already separates.
--
-- 🔴 trust IS PASSED AND IT IS THE BASELINE (section 5: 20 + 50 IP + 30 GPS). It
-- measures EVIDENCE, not outcome (M4-06), so a record with neither place-proof scores
-- the baseline and says so on the docket beside an APPROVED stamp. The number comes
-- from internal/domain/tap.TrustBase; it is a parameter rather than a literal here so
-- section 5's formula has exactly one home.
--
-- 🔴 IT IS AN INSERT ... SELECT, THE SHAPE db/queries/reviews.sql USES, AND THE SELECT
-- DOES THREE THINGS AT ONCE:
--
--	scope        the row can only be built from an employee in THIS tenant, so a
--	             foreign or invented id produces 0 rows -> pgx.ErrNoRows -> a
--	             sentence, never a foreign write (section 4.5, belt; the composite
--	             FK (employee_id, tenant_id) is the braces)
--	placement    location_id and department_id are READ FROM THE EMPLOYEE ROW rather
--	             than posted. A manager cannot mis-file somebody into another venue
--	             by editing a form, and the composite FKs on both columns are
--	             satisfied by construction because the values came from a row that
--	             already satisfies them.
--	              ⚠️ COUNTED LIMIT: somebody covering a shift at ANOTHER venue is
--	                 recorded against their HOME venue, so the venue breakdown and
--	                 the lateness shift are their usual branch's. Section 5 wants the
--	                 TAPPED location -- and there is no tapped location here. The
--	                 employee row is the only server-derived answer; a posted one
--	                 would be the manager's guess with a foreign-key check on it.
--	atomicity    the read and the write are one statement, so the placement cannot
--	             change between them.
--
-- @tenant_id APPEARS TWICE ON PURPOSE, exactly as in InsertTransactionReview: once as
-- the column being written (which the RLS WITH CHECK re-verifies) and once as the
-- SELECT's own scope predicate.
--
-- 🔴 A DEACTIVATED EMPLOYEE IS NOT EXCLUDED, and that is deliberate rather than an
-- oversight. Section 5 row 4 refuses a deactivated person's TAP because their
-- proof-of-person is void; a manual record has no session for them at all -- the
-- proof-of-person is the ADMINISTRATOR. Somebody's last shift is often typed after
-- they leave, and refusing it would create an unpayable shift with no remedy, since
-- deactivation is one-way (docs/adr/0010) and `transactions` takes no UPDATE. The
-- screen says the person is deactivated before the button is pressed.
--
-- 🔴 type IS A PARAMETER AND IT IS THE ONLY ONE THAT SHAPES THE RECORD'S MEANING.
-- It is cast to text (not sqlc.narg), so Go cannot pass NULL; transactions_type_check
-- refuses anything but 'in'/'out'; and transactions_ok_has_direction refuses a NULL
-- one anyway because verdict is the literal 'ok' above. Three layers, none of them a
-- Go `if`.
--
-- THE THREE OTHER PARAMETERS ARE CAST FOR THE SAME REASON, and it is a real
-- difference rather than decoration: trust, entered_by and employee_id are nullable
-- (entered_by) or foreign-keyed (employee_id) columns, so without the casts sqlc
-- would hand Go a *int16 and a *uuid.UUID -- i.e. it would make "no administrator on
-- this row" and "no trust score" EXPRESSIBLE from this path. They are not. `note` is
-- the one field that really is optional, and it is the only sqlc.narg here.
INSERT INTO transactions (
    tenant_id, employee_id, location_id, department_id, type, occurred_at,
    trust, verdict, note, channel, entered_by, practice, queued
)
SELECT @tenant_id, e.id, e.location_id, e.department_id, @type::text, @occurred_at,
       @trust::smallint, 'ok', sqlc.narg('note')::text, 'manual', @entered_by::uuid,
       false, false
FROM employees e
WHERE e.tenant_id = @tenant_id
  AND e.id = sqlc.arg('employee_id')::uuid
RETURNING id, occurred_at, created_at, type, verdict, channel, trust, practice, queued;

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
-- 🔴 M6-04 ADDED rv.outcome, AND IT IS THE CARD'S "the list reads the latest
-- decision through a JOIN". The verdict column keeps saying what the ENGINE decided
-- and can never say anything else (section 4.3); whether a HUMAN has since approved
-- or rejected that flag lives in transaction_reviews and is read here. The two are
-- rendered as two different things -- the stamp is the engine's, the tally is the
-- manager's -- so a decided record neither hides its flag nor pretends to still be
-- waiting.
--
-- ONE ROW AT MOST, so the LEFT JOIN cannot multiply the page: transaction_reviews
-- has UNIQUE (transaction_id) (00005, "a record is decided ONCE"). This is the only
-- join in the query whose absence of a cardinality guarantee would silently change
-- how many dockets a page holds, which is why the guarantee is named.
--
-- COST, MEASURED (EXPLAIN ANALYZE, seed tenant, ordinary day of 1 628 records,
-- warm cache, after ANALYZE, 3 repeats each after a warm-up run):
--
--	without the review join   0.745 / 0.622 / 0.463 ms
--	with it                   6.084 / 0.632 / 0.611 ms
--
-- The 6.084 ms is the first run of the changed shape and is a PLAN/CACHE artefact
-- rather than the join's price -- the two settle indistinguishably, which is what
-- an equality probe against a UNIQUE index costs on a 26-row page. The outlier is
-- printed rather than dropped because a range that excludes its own first
-- measurement is the kind this repository has had to withdraw before.
SELECT t.id, t.occurred_at, t.type, t.trust, t.verdict, t.channel, t.practice,
       t.queued, t.tag_uid, t.ctr, t.ip_match, t.gps_match, t.note,
       l.name AS location_name,
       d.name AS department_name,
       e.full_name AS employee_name,
       rv.outcome AS review_outcome,
       rv.note AS review_note
FROM transactions t
LEFT JOIN locations   l ON l.tenant_id = t.tenant_id AND l.id = t.location_id
LEFT JOIN departments d ON d.tenant_id = t.tenant_id AND d.id = t.department_id
LEFT JOIN employees   e ON e.tenant_id = t.tenant_id AND e.id = t.employee_id
LEFT JOIN transaction_reviews rv
       ON rv.tenant_id = t.tenant_id AND rv.transaction_id = t.id
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

-- name: ListWorkedShiftEvents :many
-- The REPORTS section's read (M6-07 phase A): the raw material worked hours are
-- computed from. One row per DIRECTION-CARRYING record, ordered so that one pass
-- over the result reconstructs each person's in/out chain.
--
-- 🔴 IT IS A READ AND ONLY A READ. transactions is IMMUTABLE (section 4.3). This
-- query totals nothing and stores nothing: there is no minutes_late column and no
-- hours column, and there must not be one — a stored total would be a second
-- representation of a sum the record already determines, and correcting it would
-- mean an UPDATE. The arithmetic lives in internal/domain/ledger and is recomputed
-- on every view.
--
-- 🔴 SECTION 4.7 -- WHAT THIS QUERY DELIBERATELY DOES NOT SELECT. gps_lat, gps_lng
-- and source_ip are on the row and NONE of them is in the column list, for the same
-- reason ListPanelTransactions gives: a coordinate that is never selected cannot
-- reach a template, an HTML attribute, a log line or a CSV, and phase B of this task
-- writes a CSV. policy_context is left out too — it is section 4.7-safe by
-- construction (a DISTANCE, migration 0008) but it is a replay snapshot rather than
-- report content, and selecting it would put every future key it gains into an
-- export. entered_by is left out because it names an ADMINISTRATOR and a report about
-- EMPLOYEES has no reader for one; `channel = 'manual'` is what marks a
-- manager-entered row, which is the pairing section 5 itself uses.
--
-- 🔴 AND NOTHING IS SELECTED THAT HAS NO READER. t.id orders the result and is NOT
-- in the column list; the department's NAME is not selected either, only its shift,
-- because what the report says about a department is which shift it imposed. An audit
-- found ListPanelTransactions carrying four columns nothing read, and the objection
-- was not tidiness: the paragraph above defends this screen on the ground that an
-- unselected column cannot reach an export, and columns carried for no reader are
-- what weaken it.
--
-- TENANT SCOPE (section 4.5, belt + braces): the explicit t.tenant_id predicate is
-- REQUIRED even though RLS is on, and every JOIN re-states tenant_id in its own ON
-- clause so a joined name or shift can never come from a neighbouring tenant. The
-- tenant comes from the panel session (httpx.AdminIdentity), never from the request.
--
-- WHY THE JOINS ARE LEFT JOINS: migration 0005 makes employee_id, location_id and
-- department_id nullable so a context-less record is still written (section 4.6). An
-- INNER JOIN would silently drop exactly those rows, which is a section 4.6 breach
-- committed by a read. A direction-carrying row with no employee cannot join anybody's
-- chain, so the caller counts it separately rather than dropping it silently -- and
-- today there are none. The number is the QUERY rather than a figure, because a figure
-- here goes stale on every `make test` run:
--
--      SELECT count(*) FROM transactions WHERE type IS NOT NULL AND employee_id IS NULL;
--
-- It answered 0 on 2026-08-10, over a table holding 301 819 rows. So this is a guard
-- rather than a workaround -- and a guard whose zero is worth re-checking rather than
-- inheriting.
--
-- `NOT t.practice` IS THE SAME EXCLUSION GetLastOpenTransaction CARRIES, and the
-- M6-07 card requires this query to carry it too. A practice row is a type='in' that
-- is never followed by an 'out' (ADR 0008), so leaving it in would put every new
-- starter's training tap into the manager's "needs action" queue on their first day,
-- and would open a chain that no checkout ever closes. Practice records are counted
-- separately by CountPracticeTaps below, so nothing is hidden -- they are excluded
-- from the ARITHMETIC and shown as their own tally.
--
-- `t.type IS NOT NULL` is what excludes reject and ignored without naming them, and
-- the reason it works is a CODE INVARIANT rather than the schema constraint an earlier
-- version of this line claimed.
--
-- 🔴 THERE IS NO CHECK SAYING "reject/ignored IMPLIES NO DIRECTION". Migration 0005
-- constrains the other direction only (transactions_ok_has_direction), so a directed
-- reject is a row this schema would accept. What keeps one from existing is
-- internal/domain/tap: Decide assigns a direction ONLY inside its
-- `if dec.Verdict == VerdictOK || dec.Verdict == VerdictFlag` gate (decide.go), and
-- internal/domain/checkin is the single write path. Measured on the live database:
-- zero such rows in 301 819 (2026-08-10).
--
-- So this predicate is exact TODAY and its exactness is somebody else's invariant.
-- M6-08 adds a second writer to this table; internal/domain/ledger's endpointState
-- therefore treats an unrecognised verdict as NOT payable rather than trusting that it
-- can never arrive.
--
-- THE WINDOW HAS TWO EDGES AND THEY MEAN DIFFERENT THINGS. from_at/until_at bound what
-- is READ; the reporting period the caller attributes hours to ends earlier. The tail
-- between them is how far past the period a closing tap is looked for, so that a shift
-- starting at 18:00 on the last night of the period and ending at 02:00 is a closed
-- interval rather than an anomaly. internal/domain/ledger says how long the tail is
-- and why it is the engine's own tap.StaleOpenIn.
--
-- ORDERED BY (employee_id, occurred_at, id) so ONE forward pass rebuilds every
-- person's chain: rows arrive grouped by person and in the order the taps happened.
-- The id breaks ties between two taps sharing a timestamp, so the pass is
-- deterministic. NULL employee_id sorts last (Postgres default for ASC), which keeps
-- the unattributable rows out of everybody else's chain.
--
-- THE LIMIT IS A BOUND ON THE REQUEST, NOT A PAGE. There is no cursor here: a total is
-- not something one can page through, and half a chain is a wrong total rather than a
-- partial one. The caller asks for one row more than it can use and says so on the
-- screen if that row arrives, because a silently truncated total is exactly the "state
-- something you have not measured" failure section 4.6 exists to prevent.
--
-- 🔴 THIS BLOCK NAMES NO INDEX, AND THAT IS THE CONCLUSION RATHER THAN AN OMISSION.
-- Three successive versions of it asserted which index the planner picks, and all
-- three were disproved -- twice by a re-measurement on a LATER day, and once by an
-- independent reviewer measuring on the SAME day and getting a different answer again.
-- The development database grows on every `make test` run, so its statistics move, and
-- the planner chooses between the tenant-scoped indexes accordingly. "It depends on
-- the statistics" is the honest answer, and writing a name here has only ever produced
-- a fourth wrong one.
--
-- ⚠️ SO WHAT IS RECORDED IS THE SHAPE AND THE COST, ANCHORED TO A DATE AND CARRYING ITS
-- CONDITIONS. It is one observation, not a property of the query. Re-run it before
-- believing it.
--
-- MEASURED 2026-08-10 (EXPLAIN ANALYZE, BUFFERS; warm cache; `ANALYZE transactions` run
-- first; seed tenant 10000000-...-0001, whose whole history is nine days long -- 34 061
-- rows in total, of which 24 750 carry a direction and 14 177 carry a direction and are
-- not practice). Each panel request runs THREE statements: GetTenantClock, this one and
-- CountPracticeTaps.
--
--      the week reading 12 087 rows   112.0 / 95.2 / 97.0 / 91.1 ms
--          -> a tenant-scoped bitmap index scan, then a filter that discards the rest
--             of the tenant's history: Rows Removed by Filter 17 323,
--             Heap Blocks exact=5 059
--      a week reading 0 rows          17.5 / 16.7 / 16.6 ms
--
-- ⚠️ AN INDEPENDENT RUN THE SAME DAY SAW THE OTHER INDEX and correspondingly different
-- counters (34 303 scanned, 22 109 removed). Both are the same plan SHAPE -- scan the
-- tenant, filter the week -- which is why the shape is what is written down.
--
-- ⚠️ AND A DERIVED CLAIM WENT WITH AN OLDER VERSION: it said one week was "~90% of what
-- the tenant has". Measured, it is 12 087 of 34 061 -- about 35%. The number was a
-- mislabel (a total row count quoted as a direction-carrying non-practice count) and
-- the ratio built on it inherited the error. The first run is printed rather than
-- dropped, because a range that excludes its own first measurement is the kind this
-- repository has had to withdraw before.
--
-- NO INDEX IS ADDED HERE -- that would be a migration, and nothing measured asks for
-- one: the busy figure is a development-database shape (a tenant whose entire history
-- is one week wide), and a business with a year of records behind a one-week window is
-- the selective case.
SELECT t.employee_id, t.occurred_at, t.type, t.verdict, t.channel, t.location_id,
       rv.outcome AS review_outcome,
       e.full_name AS employee_name,
       l.name AS location_name,
       l.shift_start AS location_shift_start,
       l.shift_end   AS location_shift_end,
       l.overnight   AS location_overnight,
       d.shift_start AS department_shift_start,
       d.shift_end   AS department_shift_end,
       d.overnight   AS department_overnight
FROM transactions t
LEFT JOIN transaction_reviews rv
       ON rv.tenant_id = t.tenant_id AND rv.transaction_id = t.id
LEFT JOIN employees   e ON e.tenant_id = t.tenant_id AND e.id = t.employee_id
LEFT JOIN locations   l ON l.tenant_id = t.tenant_id AND l.id = t.location_id
LEFT JOIN departments d ON d.tenant_id = t.tenant_id AND d.id = t.department_id
WHERE t.tenant_id = @tenant_id
  AND t.type IS NOT NULL
  AND NOT t.practice
  AND t.occurred_at >= @from_at
  AND t.occurred_at <  @until_at
ORDER BY t.employee_id, t.occurred_at, t.id
LIMIT sqlc.arg('row_limit')::int;

-- name: CountPracticeTaps :one
-- How many TRAINING taps fall in the reporting period (M6-07 phase A).
--
-- 🔴 IT EXISTS SO THAT "excluded" IS NOT THE SAME AS "invisible". Section 5 says a
-- practice tap never counts toward worked hours and the M6-07 card says practice rows
-- are shown SEPARATELY; ListWorkedShiftEvents drops them from the chain, so without
-- this count the report would silently differ from the transactions list by however
-- many training taps the week held. It answers one number, and the screen prints it
-- beside the sentence that says what it is not part of.
--
-- IT IS BOUNDED BY THE REPORTING PERIOD, NOT BY THE READ WINDOW: a training tap after
-- the period belongs to the next report.
--
-- TENANT SCOPE (section 4.5): explicit predicate beside RLS, tenant from the session.
SELECT count(*) AS practice_taps
FROM transactions
WHERE tenant_id = @tenant_id
  AND practice
  AND occurred_at >= @from_at
  AND occurred_at < @to_at;

-- name: TenantHasAnyTransaction :one
-- Does this business have a record -- ANY record, on ANY day (M7-03 phase B)?
--
-- 🔴 IT EXISTS TO DECIDE WHETHER ONE PIECE OF ADVICE IS WORTH GIVING, and the advice
-- is "Pick another day above" on the panel's landing section. That sentence is only
-- useful if another day could hold something; for a business with no records at all
-- it is a date picker offered as the answer to "why is this empty".
--
-- 🔴 IT IS NOT "HAS THIS BUSINESS A WORKING PLAQUE", AND THE DIFFERENCE IS A DEFECT
-- AN AUDIT MEASURED. The first version keyed the withdrawal off the plaque count, on
-- the assumption that no plaque means no record. That is false: a manager can type a
-- record by hand (channel='manual', tag_uid NULL) and an audit proved it with a real
-- INSERT into a tenant holding zero plaques. So the manager who types last week in
-- and then opens the panel would have been told nothing about the other day their
-- records are actually on. The question is asked of the records themselves.
--
-- EXISTS RATHER THAN count(*): the answer is one bit and the scan stops at the first
-- index entry, so the cost does not grow with a busy tenant's history.
--
-- COST (measured 2026-08-14, EXPLAIN ANALYZE BUFFERS, 5 runs, load 2.6-4.1; the
-- table held 128,067 rows across 25,954 tenants): Index Only Scan using
-- transactions_tenant_location_idx, shared hit=4 for the seeded KF tenant (10,193 of
-- those rows) and shared hit=3 for a tenant with none. Execution 0.069-0.184 ms (KF)
-- and 0.063-0.203 ms (empty).
--
-- ⚠️ `Heap Fetches: 0` IS AN OBSERVATION, NOT A PROPERTY, AND IT DEPENDS ON VACUUM.
-- The index-only scan can skip the heap only for pages the visibility map marks
-- all-visible, so a row nobody has vacuumed yet still costs one fetch. Measured both
-- ways on the same day: the settled KF tenant gave Heap Fetches 0 / shared hit=4,
-- and a tenant inserted seconds earlier (inside BEGIN … ROLLBACK) gave
-- **Heap Fetches: 1 / shared hit=5**. What is stable is the PLAN NODE and the bound:
-- EXISTS stops at the first row, so it is at most ONE heap page whatever the vacuum
-- state, and the cost does not grow with a busy tenant's history. That bound is what
-- makes it cheaper than the plaque count beside it, which fetched five heap blocks.
--
-- TENANT SCOPE (section 4.5): explicit predicate beside RLS.
SELECT EXISTS (
    SELECT 1 FROM transactions x WHERE x.tenant_id = @tenant_id
) AS any_record;
