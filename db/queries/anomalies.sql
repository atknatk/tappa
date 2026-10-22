-- anomalies.sql -- the reads behind the ANOMALIES section (M6-11).
--
-- WHAT THIS FILE IS FOR. The panel's other sections answer "what happened".
-- These answer "is anything here odd" -- detection, never prevention, and never a
-- judgement: every query below returns counts, names, booleans, one timestamp and two
-- row ids -- and, since M12-23/24, a date array (the days a pair tapped together) and plaque
-- counter integers (the counter values around a plaque's largest gap, with their instants) --
-- so the screen shows data and the manager draws the conclusion.
--
-- ⚠️ THAT LIST USED TO READ "COUNTS AND NAMES" AND IT UNDERCOUNTED WHAT CROSSES THE
-- BOUNDARY. The generated row types also carry `manager_typed` and `was_maskable`
-- (booleans on ListOpenCheckIns), an `occurred_at`, and a nullable employee/location id
-- on the two grouped listings. The security property is unchanged and tested; what was
-- wrong is the inventory a reader would check it against.
--
-- 🔴 SECTION 4.3 -- EVERY STATEMENT IN THIS FILE IS A SELECT. There is no UPDATE,
-- no DELETE and no INSERT, and there must not be one: `transactions` is immutable
-- and this whole section is a read of it. The property is asserted mechanically by
-- TestAnomalies_IssuesOnlySelects (internal/domain/ledger), which parses anomaly.go,
-- collects every sqlc query it names and reads each one's first keyword out of this
-- directory.
--
-- 🔴 SECTION 4.7 -- THE AGGREGATION HAPPENS IN SQL, WHICH IS A STRONGER WALL THAN
-- THE ONE THE REPORTS SECTION HAS. gps_lat, gps_lng and source_ip are on the row and
-- none of them is in any column list here; policy_context is READ but never
-- SELECTED -- it appears only inside FILTER predicates, so the jsonb value itself
-- never crosses the database boundary. That matters because policy_context legally
-- carries `tap:gpsDistanceM`: a DISTANCE rather than a coordinate (migration 0008,
-- and internal/domain/tap's gpsFacts is where the distance is computed), but a
-- snapshot that gains a key later would travel with it. Counting inside the query
-- means what Go receives is an integer, a name, a boolean, a timestamp or a row id -- plus
-- ListTapsTakenTogether's date array and ListAnomalyPlaqueJumps' plaque counter integers --
-- never the snapshot, and never a coordinate or an address.
--
-- ⚠️ AND THE ONE PLACE THAT COULD GO WRONG IS NAMED: `ip_match` and `gps_match` are
-- BOOLEANS on the row, not addresses or coordinates, and they are selected as
-- counts only. `ip_match` is "the source address fell inside a range this venue has
-- registered"; nothing below reveals which address, or how far away a reading was.
--
-- TENANT SCOPE (section 4.5, belt + braces): every statement carries an explicit
-- tenant_id predicate beside RLS, and every JOIN re-states tenant_id in its own ON
-- clause so a joined name can never come from a neighbouring tenant. The tenant
-- comes from the panel session (httpx.AdminIdentity), never from the request.
--
-- 🔴 WHY `tap:ctrGap` IS READ THROUGH A CASE RATHER THAN A BARE CAST. policy_context
-- is jsonb and NOTHING in the schema constrains its shape: migration 0008 adds the
-- column, and the only writer is the tap path. A bare `(policy_context->>'tap:ctrGap')::int`
-- therefore turns one malformed snapshot into a failed panel request for the whole
-- week -- measured: a bare cast over a text value raises
-- `invalid input syntax for type integer: "abc"`.
--
-- ⚠️ THE REASON A `jsonb_typeof(...) = 'number' AND (...)::numeric` GUARD IS NOT USED
-- IS EVALUATION ORDER, NOT SHORT-CIRCUITING, and this comment said the wrong thing
-- first. Measured 2026-08-12: the AND-guarded form did NOT raise on the same bad row.
-- That is not a guarantee, though -- PostgreSQL does not promise the order in which
-- the operands of AND are evaluated, and the planner may reorder them. CASE is the
-- construct that DOES promise it evaluates only the branch it selects, so the guard is
-- written as one. The claim is about what is guaranteed, not about what was observed.
--
-- Measured on the development database 2026-08-12: every stored value is an integer.
--
--      SELECT DISTINCT policy_context->>'tap:ctrGap' FROM transactions
--       WHERE policy_context ? 'tap:ctrGap';
--
-- ⚠️ THE OBSERVED VALUES ARE NOT LISTED HERE. This comment enumerated them and the list
-- went stale inside one working day -- two of the values it named were added by this
-- section's OWN test fixture. What is durable is the CONCLUSION (every value is a
-- number) and the QUERY that re-checks it; what is not durable is the set.
--
-- So this is a guard rather than a workaround, and it costs one CASE.

-- name: CountAnomalySignals :one
-- The window in one row: how much was examined, and how many records carry each
-- signal.
--
-- 🔴 THE DENOMINATOR IS RETURNED BESIDE EVERY NUMERATOR, which is the difference
-- between a report and an accusation. "47 taps matched on GPS alone" means nothing
-- without the base it came out of -- and there are TWO bases here, which is exactly
-- why the example has to use two different numbers: "47 of 2 100 records the engine
-- judged on evidence", beside "3 of 1 927 records that carry a rule snapshot". The
-- screen prints both bases and every figure names the one it is out of.
--
-- ⚠️ `judged` IS NOT `examined`. A record with neither ip_match nor gps_match set is
-- one the engine never gathered address or distance evidence for -- a manager-typed
-- row is the ordinary case (section 5: channel='manual' carries no evidence of a
-- physical touch). Counting those in the denominator would quietly dilute every rate
-- on the page.
--
-- 🔴 `unanswerable` IS THE SECOND DENOMINATOR, NOT A FOOTNOTE, AND THE FIRST DRAFT OF
-- THIS SECTION GOT IT WRONG IN THE ONE WAY THAT MATTERS. Three of the signals below --
-- counter_gaps, outside_venue and other_venue -- can only be read out of policy_context,
-- which exists on a record only since migration 0008 and is written only by the tap
-- path (M5-05). A record without one is not a record without a counter gap; it is a
-- record this screen CANNOT ANSWER FOR.
--
-- ⚠️ SO PUTTING IT IN THOSE THREE FIGURES' DENOMINATOR IS, ARITHMETICALLY, COUNTING IT
-- AS CLEAN -- which is the exact thing the sentence above forbids. The first draft
-- printed `1 of 22 after a counter gap` on a window where only 18 records could be
-- asked the question, and printed "they are not counted as clean" directly underneath.
-- The caller now divides those three by `records - unanswerable`, and divides
-- distance_only (which comes from COLUMNS, present on every judged row) by `judged`.
-- Two bases, and the screen names which is which.
--
-- TRAINING TAPS ARE COUNTED HERE. A practice tap is a real physical touch and its
-- evidence is real evidence, so excluding it from a signal count would hide a
-- genuine anomaly behind a new starter. They are excluded from ONE thing only -- the
-- open check-in list -- for the reason the M6-11 card gives.
-- 🔴 M11 API-1c (the second review's ORTA-A, K-39): the EVIDENCE figures -- judged (the base of
-- distance_only), distance_only, outside_venue and outside_venue_on_network -- count the business's
-- OWN venues only (site_tenant_id IS NULL): a row at a holding peer's venue was judged against the
-- PEER's network and coordinate, which this screen does not describe even as a count. Those rows
-- are counted apart (partner_records, partner_answerable -- the second the share of the snapshot
-- base that is a partner's), so a figure out of a base never mixes the two. The non-evidence figures
-- (counter_gaps -- K-1i hands the employer the gap --, other_venue, training, manager_typed,
-- unanswerable) count every row. The inventory is TestEvidenceReaders_EveryOneIsClassified.
SELECT
    count(*)                                                       AS records,
    count(*) FILTER (
        WHERE (t.ip_match IS NOT NULL OR t.gps_match IS NOT NULL) AND t.site_tenant_id IS NULL
    )                                                              AS judged,
    count(*) FILTER (WHERE t.policy_context IS NULL)               AS unanswerable,
    -- Verified by distance alone: a reading inside the venue's radius, with no
    -- address match to corroborate it.
    count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) AS distance_only,
    -- A reading was taken and it was OUTSIDE the venue. This is NOT "the address
    -- matched but the distance did not" -- see internal/domain/tap's gpsFacts, where
    -- the flag is set from the distance alone.
    count(*) FILTER (
        WHERE t.policy_context ->> 'tap:gpsConflict' = 'true' AND t.site_tenant_id IS NULL
    )                                                              AS outside_venue,
    -- The subset of those that ALSO matched the venue's registered address range,
    -- which is the shape a device left behind on the premises would produce.
    count(*) FILTER (
        WHERE t.policy_context ->> 'tap:gpsConflict' = 'true' AND t.ip_match AND t.site_tenant_id IS NULL
    )                                                              AS outside_venue_on_network,
    count(*) FILTER (
        WHERE CASE
                  WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
                  THEN (t.policy_context -> 'tap:ctrGap')::numeric
                  ELSE 0
              END > 0
    )                                                              AS counter_gaps,
    count(*) FILTER (
        WHERE t.policy_context ->> 'employee:crossLocation' = 'true'
    )                                                              AS other_venue,
    count(*) FILTER (WHERE t.practice)                             AS training,
    count(*) FILTER (WHERE t.channel = 'manual')                   AS manager_typed,
    count(*) FILTER (WHERE t.site_tenant_id IS NOT NULL)            AS partner_records,
    count(*) FILTER (
        WHERE t.site_tenant_id IS NOT NULL AND t.policy_context IS NOT NULL
    )                                                              AS partner_answerable
FROM transactions t
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at;

-- name: ListAnomalyPeople :many
-- One row per employee who carries at least one signal in the window.
--
-- 🔴 THE `HAVING` IS WHAT KEEPS THIS FROM BEING A ROSTER. A list of everybody with a
-- zero in every column is the employees section with extra steps, and it would make
-- the reader hunt. Only rows with something on them are returned -- which is a
-- FILTER on the data, not a verdict about the person: the columns say what was
-- counted and the screen adds no word of its own.
--
-- LEFT JOIN, because migration 0005 lets a record name an employee the roster no
-- longer holds; the caller prints "Unknown employee" rather than dropping the row.
SELECT
    t.employee_id                                                  AS employee_id,
    e.full_name                                                    AS employee_name,
    count(*)                                                       AS records,
    count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) AS distance_only,
    count(*) FILTER (
        WHERE t.policy_context ->> 'tap:gpsConflict' = 'true' AND t.site_tenant_id IS NULL
    )                                                              AS outside_venue,
    count(*) FILTER (
        WHERE CASE
                  WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
                  THEN (t.policy_context -> 'tap:ctrGap')::numeric
                  ELSE 0
              END > 0
    )                                                              AS counter_gaps,
    count(*) FILTER (
        WHERE t.policy_context ->> 'employee:crossLocation' = 'true'
    )                                                              AS other_venue
FROM transactions t
LEFT JOIN employees e
       ON e.id = t.employee_id
      AND e.tenant_id = t.tenant_id
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND t.employee_id IS NOT NULL
GROUP BY t.employee_id, e.full_name
HAVING count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) > 0
    OR count(*) FILTER (WHERE t.policy_context ->> 'tap:gpsConflict' = 'true' AND t.site_tenant_id IS NULL) > 0
    OR count(*) FILTER (
           WHERE CASE
                     WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
                     THEN (t.policy_context -> 'tap:ctrGap')::numeric
                     ELSE 0
                 END > 0
       ) > 0
    OR count(*) FILTER (WHERE t.policy_context ->> 'employee:crossLocation' = 'true') > 0
ORDER BY
    count(*) FILTER (
        WHERE CASE
                  WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
                  THEN (t.policy_context -> 'tap:ctrGap')::numeric
                  ELSE 0
              END > 0
    ) DESC,
    count(*) FILTER (WHERE t.policy_context ->> 'tap:gpsConflict' = 'true' AND t.site_tenant_id IS NULL) DESC,
    count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) DESC,
    e.full_name,
    t.employee_id
LIMIT @row_limit;

-- name: ListAnomalyVenues :many
-- The same window split by the venue whose plaque was touched.
--
-- IT IS THE TAPPED LOCATION rather than the employee's home one (section 5, Q17):
-- the question "is this venue's WiFi failing" is a question about the door somebody
-- stood at. Every venue with taps in the window is returned, including the clean
-- ones -- unlike the people list, because a rate of zero at one branch beside a rate
-- of a third at another is the comparison this section exists to make.
-- 🔴 M11 API-1c: A ROW TAKEN AT A PEER'S VENUE IS COUNTED UNDER THAT VENUE (ADR 0027 §9, one of
-- the employer's nine readers): its location_id is NULL and site_location_id names the peer's
-- venue (K-30), so the group is the one that is set. The peer's name is not in this tenant's
-- locations -- l.name is NULL for that group, and internal/domain/ledger names it from
-- holding_site_locations. K-39: the EVIDENCE figures (distance_only, outside_venue and, since M12-22,
-- both_matched, network_only, neither_matched) count the employer's own venues only -- a peer's
-- network and coordinate are not this panel's to describe, even as a count of matches.
--
-- 🔴 M12-22 -- THE FOUR-WAY SPLIT OF `judged` (the Odd shapes "evidence mix" bar). Four figures,
-- each a count over the two evidence BOOLEANS (never an address, never a distance -- §4.7), each
-- carrying `t.site_tenant_id IS NULL` (K-39: a partner venue's row has no bar at all):
--
--      both_matched     ip_match AND gps_match
--      network_only     ip_match AND NOT coalesce(gps_match, false)   -- gps false OR not taken
--      distance_only    gps_match AND NOT ip_match                    -- UNCHANGED (M6-11)
--      neither_matched  (ip_match IS NOT NULL OR gps_match IS NOT NULL)
--                       AND NOT coalesce(ip_match, false) AND NOT coalesce(gps_match, false)
--
-- Read against `judged` (one of the two set) they are disjoint, and they cover it EXCEPT one
-- shape, named rather than folded somewhere: ip_match NULL with gps_match true. distance_only
-- (whose definition this card keeps) does not count it -- `NOT NULL` is NULL --, and it is not a
-- match of the network, nor "neither". No writer produces it: the tap path writes both booleans
-- together (internal/domain/checkin: IpMatch and GpsMatch from one Decision), the manual path
-- writes neither. Measured 2026-10-20 on the c4 database (KF's simulated day + the test rows):
-- the combinations present are (f,f) (f,t) (t,f) (t,t) and (NULL,NULL) only. internal/domain/
-- ledger's AnomalyVenue.Unsplit() is the remainder, so a row of that shape would show as "not
-- split" rather than be spread over a segment it is not.
--
-- ⚠️ `judged` ITSELF DOES NOT CARRY THE SITE FILTER, and need not: a venue row is the employer's
-- own (location_id) OR a peer's (site_location_id) -- never both (the
-- transactions_site_excludes_the_local_venue CHECK) -- so a partner row's judged is a count of
-- its records the screen never divides (no bar for a partner row), and an own row's judged is
-- exactly the base the four figures split.
--
-- EXPLAIN ANALYZE (2026-10-20, seeded KF 10000000-…-0001, the last 7 days: 22 rows): Limit ->
-- Sort -> GroupAggregate -> Sort -> Nested Loop Left Join (Bitmap Index Scan on
-- transactions_tenant_occurred_idx; locations by locations_id_tenant_key) -- the three FILTERs add
-- no node, they are evaluated in the one aggregate pass.
SELECT
    coalesce(t.location_id, t.site_location_id)::uuid              AS location_id,
    l.name                                                         AS location_name,
    count(*)                                                       AS records,
    count(*) FILTER (
        WHERE t.ip_match IS NOT NULL OR t.gps_match IS NOT NULL
    )                                                              AS judged,
    -- THE SECOND BASE, PER VENUE. outside_venue is read out of the snapshot and
    -- distance_only out of the columns, so the two figures on a venue's row are out of
    -- DIFFERENT populations. Returning both bases is what lets the screen say so
    -- instead of quietly dividing by whichever one is to hand.
    count(*) FILTER (WHERE t.policy_context IS NOT NULL)            AS answerable,
    count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) AS distance_only,
    count(*) FILTER (
        WHERE t.policy_context ->> 'tap:gpsConflict' = 'true' AND t.site_tenant_id IS NULL
    )                                                              AS outside_venue,
    count(*) FILTER (
        WHERE t.ip_match AND t.gps_match AND t.site_tenant_id IS NULL
    )                                                              AS both_matched,
    count(*) FILTER (
        WHERE t.ip_match AND NOT coalesce(t.gps_match, false) AND t.site_tenant_id IS NULL
    )                                                              AS network_only,
    count(*) FILTER (
        WHERE (t.ip_match IS NOT NULL OR t.gps_match IS NOT NULL)
          AND NOT coalesce(t.ip_match, false)
          AND NOT coalesce(t.gps_match, false)
          AND t.site_tenant_id IS NULL
    )                                                              AS neither_matched
FROM transactions t
LEFT JOIN locations l
       ON l.id = t.location_id
      AND l.tenant_id = t.tenant_id
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND (t.location_id IS NOT NULL OR t.site_location_id IS NOT NULL)
GROUP BY coalesce(t.location_id, t.site_location_id), l.name
ORDER BY
    count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) DESC,
    l.name,
    coalesce(t.location_id, t.site_location_id)
LIMIT @row_limit;

-- name: ListAnomalyPlaques :many
-- Counter gaps per plaque -- the OTHER half of the ctr-gap signal (A1 / Q21).
--
-- 🔴 A GAP IS A PROPERTY OF THE PLAQUE'S COUNTER, NOT OF THE PERSON. The NTAG chip
-- increments `ctr` on every read whether or not the resulting page is ever posted,
-- so a run of unposted reads shows up as a jump the next time somebody does tap
-- (CLAUDE.md section 4.4, and the atomic advance in tags.sql is where the gap is
-- measured). Breaking it down by employee alone would attribute one door's collected
-- URLs to whoever happened to walk through it next.
--
-- `largest_gap` is the biggest single jump rather than a sum: ten taps each one
-- short is an ordinary busy plaque, one tap forty short is not the same event.
SELECT
    t.tag_uid                                                      AS tag_uid,
    l.name                                                         AS location_name,
    count(*)                                                       AS records,
    count(*) FILTER (
        WHERE CASE
                  WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
                  THEN (t.policy_context -> 'tap:ctrGap')::numeric
                  ELSE 0
              END > 0
    )                                                              AS counter_gaps,
    coalesce(max(
        CASE
            WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
            THEN (t.policy_context -> 'tap:ctrGap')::numeric
            ELSE 0
        END
    ), 0)::int                                                     AS largest_gap
FROM transactions t
LEFT JOIN locations l
       ON l.id = t.location_id
      AND l.tenant_id = t.tenant_id
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND t.tag_uid IS NOT NULL
GROUP BY t.tag_uid, l.name
HAVING count(*) FILTER (
           WHERE CASE
                     WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
                     THEN (t.policy_context -> 'tap:ctrGap')::numeric
                     ELSE 0
                 END > 0
       ) > 0
ORDER BY largest_gap DESC, counter_gaps DESC, t.tag_uid
LIMIT @row_limit;

-- name: ListAnomalyPlaqueJumps :many
-- The counter values around each listed plaque's LARGEST gap (M12-24, Odd shapes "#from -> #to").
--
-- 🔴 WHAT IT READS IS THE CHIP'S COUNTER, AND ONLY FOR THIS BUSINESS'S OWN PLAQUES. `ctr` is a
-- chip fact (the evidence inventory lists it), so the statement is filed as `excluded`:
-- `t.tag_uid IS NOT NULL` keeps every row taken at a holding peer's venue out -- such a row's
-- tag_uid is NULL (K-30), and a peer's plaque is never this business's anomaly (ListAnomalyPlaques'
-- own reason). The caller passes the uids ListAnomalyPlaques (merged with the projection's gaps)
-- listed, so the cap is that listing's and this adds no row of its own.
--
-- PER (plaque, venue name) -- ListAnomalyPlaques' grouping -- the window's read with the largest
-- gap (the same CASE guard, so a malformed snapshot is a 0, never an error), ties broken by the
-- higher counter. Its `ctr` is the "to"; the caller derives "from" = to - gap - 1 (the counter of
-- the read the atomic advance compared against, tags.sql).
--
-- THE PREVIOUS READ -- the latest earlier read of the same plaque whose SUN verified (a replayed
-- URL's row carries an old counter at a later time and would date the jump wrongly; sun_valid is
-- set only when the counter advanced). It may precede the window, so it is a per-plaque lookup bounded
-- below by @lookback_from (the caller's: the window's start less ledger.CounterJumpLookback) --
-- without the bound a plaque with no earlier read would walk the tenant's whole history. The
-- caller uses its time ONLY when its counter IS "from"; any other counter means the read just
-- before the gap is not on this business's record (an activation tap writes no row -- ADR 0026 --,
-- a holding peer's read is in the peer's books) and the screen says so rather than pairing a
-- counter with another read's time.
--
-- ⚠️ BOTH TIMES ARE THE DECLARED occurred_at (a phone's claim; ADR 0006 measures the server's
-- clock only for debounce) -- the screen says "between", with that caveat.
--
-- EXPLAIN ANALYZE (2026-10-20, c4 database): seeded KF (22 rows in the last 7 days, no gap, its 11
-- plaques asked) -> Sort -> Hash Left Join [Hash Cond (SubPlan 1) = p.id AND tag_uid] -> Unique ->
-- Sort -> Nested Loop Left Join (locations) over an Index Scan on transactions_tenant_occurred_idx
-- (0 rows qualify); the Hash's inner side is an Index Scan on transactions_tenant_occurred_idx from
-- the lookback bound on (never executed). On TestAnomalies_ThePlaqueJumpNamesItsCounters' tenant (4
-- gap rows, 3 jumps): the same shape, the SubPlan a Limit over an Index Scan on
-- transactions_tenant_occurred_idx bounded below by the lookback (1 row per loop), 1.2 ms.
WITH jump AS (
    SELECT DISTINCT ON (t.tag_uid, l.name)
        t.tag_uid                                                  AS tag_uid,
        l.name                                                     AS location_name,
        t.ctr                                                      AS revealed_ctr,
        t.occurred_at                                              AS revealed_at,
        (CASE
             WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
             THEN (t.policy_context -> 'tap:ctrGap')::numeric
             ELSE 0
         END)::int                                                 AS gap
    FROM transactions t
    LEFT JOIN locations l
           ON l.id = t.location_id
          AND l.tenant_id = t.tenant_id
    WHERE t.tenant_id = @tenant_id
      AND t.occurred_at >= @from_at
      AND t.occurred_at < @to_at
      AND t.tag_uid IS NOT NULL
      AND t.tag_uid = ANY (@tag_uids::text[])
      AND t.ctr IS NOT NULL
      AND CASE
              WHEN jsonb_typeof(t.policy_context -> 'tap:ctrGap') = 'number'
              THEN (t.policy_context -> 'tap:ctrGap')::numeric
              ELSE 0
          END > 0
    ORDER BY t.tag_uid, l.name, gap DESC, t.ctr DESC
), placed AS (
    -- The previous read's id, once per jump (at most one per listed plaque): the latest earlier
    -- verified read of the same plaque inside the lookback.
    SELECT
        j.tag_uid, j.location_name, j.revealed_ctr, j.revealed_at, j.gap,
        (
            SELECT q.id
            FROM transactions q
            WHERE q.tenant_id = @tenant_id
              AND q.tag_uid = j.tag_uid
              AND q.sun_valid
              AND q.ctr < j.revealed_ctr
              AND q.occurred_at >= @lookback_from
              AND q.occurred_at <= j.revealed_at
            ORDER BY q.occurred_at DESC, q.ctr DESC
            LIMIT 1
        )                                                          AS previous_id
    FROM jump j
)
SELECT
    j.tag_uid::text                                                AS tag_uid,
    j.location_name                                                AS location_name,
    j.revealed_ctr                                                 AS revealed_ctr,
    j.revealed_at                                                  AS revealed_at,
    j.gap                                                          AS gap,
    p.ctr                                                          AS previous_ctr,
    p.occurred_at                                                  AS previous_at
FROM placed j
-- THE PREVIOUS READ IS JOINED BACK BY ITS KEY rather than selected out of a LEFT JOIN LATERAL, for
-- one measured reason: sqlc v1.28 types a LATERAL's columns as NOT NULL (time.Time, which a
-- missing previous read fails to scan into) and a CASE with a NULL arm as interface{} (the open
-- type the anomaly type wall refuses); a LEFT JOIN of the table itself is typed nullable. The
-- tenant is re-stated in the subquery and in the ON.
-- ⚠️ THE ON REPEATS THE LOOKBACK BOUND, redundantly for the row it finds, because the planner
-- chooses a HASH join here whatever shape the pick takes, and the hash's inner side is every row
-- the ON lets through: with the key and the tenant alone that was the tenant's whole history
-- (measured, EXPLAIN ANALYZE 2026-10-20: Bitmap Heap Scan of all the tenant's rows into the
-- Hash). With the bound it is the tenant's rows inside the lookback window -- of every plaque, not
-- just the listed ones: the key and tag_uid are the Hash Cond, matched while probing, and only the
-- tenant and the bound filter what goes into the hash (the plan in the header above).
LEFT JOIN transactions p
       ON p.id = j.previous_id
      AND p.tenant_id = @tenant_id
      AND p.tag_uid = j.tag_uid
      AND p.occurred_at >= @lookback_from
ORDER BY j.tag_uid, j.location_name;

-- name: ListAnomalyPolicySids :many
-- Which rule decided how many records in the window (M3-07).
--
-- 🔴 IT IS THE ONE BREAKDOWN ON THIS SCREEN THAT IS NOT A SUSPICION. A guardrail
-- firing is the system working; the number is here because a CHANGE in the mix is
-- the thing worth seeing -- a week where sys:sun-invalid triples is a week where a
-- plaque is failing or somebody is replaying URLs, and neither is visible in a
-- verdict count.
--
-- matched_sid IS NULL is kept and named rather than dropped: migration 0008's CHECK
-- allows a record with no policy decision at all (everything written before M3-07,
-- and every manager-typed row), and folding those into another bucket would make the
-- column sum disagree with the record count above it.
-- M11 API-1c (K-39; the third review's ORTA): a row at a holding peer's venue is NOT in this
-- breakdown at all -- which rule decided it (gps-only-allow, no-evidence-review, the cross-tenant QR
-- guardrail ...) says what the peer's network and coordinate showed, and so would the place a
-- folded bucket took in an order by count and sid (measured: two partner rules of the same count
-- sorted the bucket before or after the employer's own rule by the partner rule's name). The reader
-- shows them as one bucket, ALWAYS LAST, counted from CountAnomalySignals' partner_records; leaving
-- them out here also keeps them from spending the row limit. The order is total: count, sid, layer.
SELECT
    t.matched_sid                                                  AS matched_sid,
    t.policy_layer                                                 AS policy_layer,
    count(*)                                                       AS records
FROM transactions t
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND t.site_tenant_id IS NULL
GROUP BY t.matched_sid, t.policy_layer
ORDER BY count(*) DESC, t.matched_sid NULLS FIRST, t.policy_layer NULLS FIRST
LIMIT @row_limit;

-- name: ListTapsTakenTogether :many
-- Pairs of people whose taps land at the same venue within seconds of each other, on
-- more than one day (the buddy-punching signal).
--
-- 🔴 IT IS ADJACENT PAIRS OVER AN ORDERED STREAM, NOT A SELF-JOIN, AND THE ARGUMENT
-- IS ABOUT HOW MANY COMPARISONS EACH SHAPE MAKES -- NOT ABOUT MILLISECONDS. The
-- obvious shape (join the window to itself on "same location, later, within N
-- seconds") makes a number of comparisons that grows with the SQUARE of the week's
-- rows, and the query plan reports that number itself:
--
--      self-join, plain `b.location_id = a.location_id`, week bounds on both sides
--        -> Hash Join, then `Rows Removed by Join Filter: 1 560 627`
--           over a window holding 1 365 directed non-practice rows (1 365^2 = 1 863 225,
--           so the filter is evaluating ~84% of a full cartesian: the venue equality
--           becomes a hash key and one venue still holds most of the rows)
--      lag() over (PARTITION BY location_id ORDER BY occurred_at, id)
--        -> one Sort + one WindowAgg over the same 1 365 rows
--
-- Measured 2026-08-12, tenant Kebab Factory, the seven local days to 2026-08-13,
-- EXPLAIN (ANALYZE, BUFFERS).
--
-- 🔴 NO SPEED RATIO IS CLAIMED, AND THAT IS A CORRECTION RATHER THAN MODESTY. This
-- block has now quoted a wall-clock ratio TWICE and been wrong twice: first
-- "151.3 ms vs 5.6 ms, 27x" (single warm readings, did not survive a re-run), then
-- "209.8/221.3/252.8 ms vs 11.0-24.7 ms, ten to twenty times" -- which an independent
-- reviewer could not reproduce, measuring the same shape at 17-19 ms on a comparable
-- table, roughly six times faster than this machine saw it; a third reading, taken
-- later again, differed from both. Measurements of one statement disagreeing by that
-- much mean the timing is a property of the machine and the cache, not of the query.
--
-- ⚠️ AND WHAT IS STABLE IS THE PROPORTION RATHER THAN THE COUNT -- this line claimed
-- the count itself was "the same on any machine" and it is not. Three readings of this
-- comparison used THREE DIFFERENT JOIN STRATEGIES (hash, nested loop, merge) over
-- windows of 1 063, 1 365 and 1 547 directed rows, because the table grows on every
-- `make test` run. The absolute "rows removed" figure moves with all of that. What does
-- NOT move is that the join filter evaluates a large FRACTION of n^2 -- 1 560 627 of
-- 1 863 225, about 84%, in the reading above -- while the window function makes one
-- pass over n rows. The fraction is the argument; the count is one observation of it.
--
-- ⚠️ WHAT THE CHEAP SHAPE CANNOT SEE, COUNTED RATHER THAN GLOSSED. lag(1) compares
-- each tap with the ONE before it at that venue, so in a burst A, B, C it reports
-- (A,B) and (B,C) but never (A,C). The cluster is still visible -- both of its
-- neighbouring pairs are listed -- but the specific first-to-last pair is not, and a
-- reader counting pairs will find fewer than a full cross-product would give.
--
-- 🔴 SECTION 5 SAYS A QUEUE IS NORMAL: "different people may touch the same plaque
-- one after another". So a single burst is not a signal at all, and `min_days` is
-- what separates the two -- people who arrive together every day, rather than people
-- who happened to queue once. The threshold is the caller's and is printed on the
-- screen beside the list.
--
-- PRACTICE AND UNDIRECTED ROWS ARE EXCLUDED. A rejected touch never became an
-- attendance record, and a training tap is a first-day event; either would put pairs
-- in this list that describe nothing about a working day.
WITH ordered AS (
    SELECT
        -- CARRIED SO THE NEXT BLOCK CAN RE-STATE THE SCOPE. `pairs` reads this CTE
        -- rather than a table, so its own WHERE would otherwise have nothing to bind
        -- the tenant to -- and section 4.5's belt is a per-block property, not a
        -- per-statement one. It costs one column and it means every WHERE in this
        -- statement says the same thing.
        -- M11 API-1c: a row at a PEER's venue joins that venue's sequence (ADR 0027 §9: this
        -- reader left it out before) -- its location_id is NULL, site_location_id names the venue.
        t.tenant_id,
        t.employee_id,
        coalesce(t.location_id, t.site_location_id) AS location_id,
        t.occurred_at,
        lag(t.employee_id) OVER (
            PARTITION BY coalesce(t.location_id, t.site_location_id) ORDER BY t.occurred_at, t.id
        ) AS prev_employee_id,
        lag(t.occurred_at) OVER (
            PARTITION BY coalesce(t.location_id, t.site_location_id) ORDER BY t.occurred_at, t.id
        ) AS prev_occurred_at
    FROM transactions t
    WHERE t.tenant_id = @tenant_id
      AND t.occurred_at >= @from_at
      AND t.occurred_at < @to_at
      AND t.employee_id IS NOT NULL
      AND (t.location_id IS NOT NULL OR t.site_location_id IS NOT NULL)
      AND t.type IS NOT NULL
      AND NOT t.practice
), pairs AS (
    SELECT
        least(o.employee_id, o.prev_employee_id)                   AS first_id,
        greatest(o.employee_id, o.prev_employee_id)                AS second_id,
        o.location_id,
        (o.occurred_at AT TIME ZONE @zone::text)::date             AS local_day,
        extract(epoch FROM (o.occurred_at - o.prev_occurred_at))   AS apart_seconds
    FROM ordered o
    WHERE o.tenant_id = @tenant_id
      AND o.prev_employee_id IS NOT NULL
      AND o.prev_employee_id <> o.employee_id
      AND o.occurred_at - o.prev_occurred_at
            < make_interval(secs => @within_seconds::double precision)
)
-- ⚠️ THE TWO EMPLOYEE IDS ARE GROUPED ON AND ORDERED BY, AND DELIBERATELY NOT
-- SELECTED. Two reasons, and the first is the file's own rule: nothing is selected
-- that has no reader, and this screen links nowhere -- it names people, it does not
-- offer an action on them. The second is measured: `least(uuid, uuid)` is a shape
-- sqlc v1.28 cannot type, and it emitted `interface{}` for both columns. An
-- `interface{}` on a view type is precisely the field a coordinate could travel in
-- that section 4.7's name-based net cannot see, so the honest fix is to not carry
-- the column rather than to cast it into place.
--
-- ⚠️ `together` IS SELECTED AND IT IS RENDERED. A review found it reaching
-- ledger.AnomalyPair and stopping there -- selected with no reader, which is the rule
-- this very paragraph states, broken two lines below where it is written. It is on the
-- screen now ("together N times"), because "twice on two days" and "forty times on two
-- days" are different rows and the day count alone cannot tell them apart.
--
-- M12-23 -- `together_days` IS WHICH LOCAL DAYS, BESIDE `days` (HOW MANY). The same
-- `local_day` the HAVING counts -- bucketed in the zone the caller resolved -- so its length
-- is `days` by construction, and the 20 s / same venue / 2+ days definition is untouched (no
-- predicate changed). internal/domain/ledger maps it against Period.DayLabels into one
-- boolean per day of the week. It reads no evidence column: a pair is two people's times at
-- one venue. EXPLAIN ANALYZE (2026-10-20, seeded KF after a simulated day, the last 7 days,
-- min_days 1 so the probe has groups: 19 rows in, 8 pairs out): Limit -> Sort -> GroupAggregate
-- -> Sort -> Nested Loop Left Join (locations) over two Hash Right Joins (employees b, a; seq
-- scans of the tenant's 69 people) over Subquery Scan `o` -> WindowAgg -> Sort -> Index Scan on
-- transactions_tenant_occurred_idx. The array is built in the GroupAggregate that already sorts
-- by local_day for count(DISTINCT) -- no node added.
SELECT
    a.full_name                                                    AS first_employee_name,
    b.full_name                                                    AS second_employee_name,
    l.name                                                         AS location_name,
    p.location_id::uuid                                            AS location_id,
    count(*)                                                       AS together,
    count(DISTINCT p.local_day)                                    AS days,
    min(p.apart_seconds)::int                                      AS closest_seconds,
    array_agg(DISTINCT p.local_day ORDER BY p.local_day)::date[]   AS together_days
FROM pairs p
LEFT JOIN employees a ON a.id = p.first_id  AND a.tenant_id = @tenant_id
LEFT JOIN employees b ON b.id = p.second_id AND b.tenant_id = @tenant_id
LEFT JOIN locations l ON l.id = p.location_id AND l.tenant_id = @tenant_id
GROUP BY p.first_id, a.full_name, p.second_id, b.full_name, l.name, p.location_id
HAVING count(DISTINCT p.local_day) >= @min_days::int
ORDER BY
    count(DISTINCT p.local_day) DESC,
    count(*) DESC,
    a.full_name,
    b.full_name,
    p.first_id,
    p.second_id
LIMIT @row_limit;

-- name: ListOpenCheckIns :many
-- Check-ins in the window that no checkout closes.
--
-- 🔴 "OPEN" IS ADR 0032 §A.6's DEFINITION, THE ONE THE REPORTS SECTION USES TOO (M15-05).
-- A non-practice `in` is open exactly when the person's NEXT directed, non-practice
-- record after it -- in (occurred_at, id) order -- does not CLOSE it: there is none, it
-- is another `in`, or it is an `out` whose class is not 'closes'. The class is
-- tappa_checkout_class (migration 00064), the one SQL form of the classification;
-- internal/domain/ledger reads the same rows through tap.ClassifyCheckout, its Go form,
-- and the two are bound by a parity test (internal/domain/ledger's checkout parity
-- test). So this list and the report's "Open" entries are the same set for the same
-- window -- the "legitimate difference" this comment used to defend is gone.
--
-- WHAT THE OLD LINE GOT WRONG, MEASURED BEFORE IT WAS REPLACED: it said "no `out` after
-- it, full stop". A single `out` then closed EVERY older unclosed check-in at once --
-- Monday's `in`, Tuesday's `in`, Tuesday's `out` took Monday off the list although
-- nothing closed it (ADR 0032 §A.6) -- and an `out` 30 hours later, or one of another
-- business day, closed it too, where the report counted the entry open (tap.StaleOpenIn).
--
-- ⚠️ THE NEXT RECORD IS LOOKED FOR AMONG IN AND OUT ALIKE, NOT AMONG CHECKOUTS. A later
-- `in` is what leaves this one open (the report's "a second `in` on top of an open one"),
-- so a lookup over `out` rows alone would skip it and let the checkout after it close
-- the wrong entry -- the old defect again, one step removed.
--
-- ⚠️ THE ID BREAKS THE TIE, ON BOTH SIDES OF THE COMPARISON. Two records at one
-- occurred_at are ordered by id -- the order ListWorkedShiftEvents, GetLastOpenTransaction
-- and the classification read (ADR 0032 §A.4) -- so an `in` and an `out` sharing an
-- instant pair the same way in every reader.
--
-- THE CLASS OF THAT NEXT `out` IS COMPUTED WITH P = THIS `in` -- by construction it is the
-- record before it -- so tappa_checkout_class takes this row's occurred_at as prev_in_at
-- and reads only the two closing bounds (the out's business day, 18 hours). It does not
-- read prev_is_out, day_has_in or prev_written_later when P is an `in`; they are passed
-- as false, the value the function would read them as anyway.
--
-- ⚠️ `NOT t.practice` IS THE M5-07 AUDIT'S RULE AND IT IS LOAD-BEARING HERE. A
-- practice row is a type='in' that is never followed by an 'out' (ADR 0008), so
-- without this predicate the list would produce exactly one false positive per new
-- starter -- and "the report does not comment, it shows data" would be broken by the
-- report's own arithmetic. The next-record lookup skips practice rows for the same
-- reason the report's read drops them.
--
-- 🔴 `was_maskable` IS THE DEVIR NOTE FROM M5-11 MADE VISIBLE. Before ADR 0008 a
-- practice row could sort on top of a real open check-in and make the next tap resolve
-- to `in` instead of `out`. Those rows are still on disk (transactions is immutable)
-- and they cannot be told apart from an ordinary forgotten checkout by their own
-- columns -- but "does a practice row of this person's sit AFTER this check-in" can be
-- asked, and that is what this flag answers. It is a HINT rather than a diagnosis, and
-- the screen says so. Since M15-05 such an entry STAYS on this list: the `in` the mask
-- produced is its next record, and an `in` closes nothing.
--
-- ORDERED OLDEST FIRST, which is the order a manager works a backlog in, so a
-- shortened page keeps the entries that have been waiting longest.
-- M11 API-1c: site_location_id names the venue of an open check-in taken at a PEER's venue (its
-- location_id is NULL -- K-30); internal/domain/ledger names it from holding_site_locations.
--
-- M15-07: employee_id is returned for the business-day settings screen, which counts the
-- check-ins open RIGHT NOW that a new cutoff would split, among the people who inherit it
-- (ADR 0032 §A.7). It reads this list rather than a copy of its definition, so "open" on
-- that screen is this list's "open"; the column names a person by opaque id, never a name.
--
-- EXPLAIN (ANALYZE, BUFFERS): M15-05's before/after measurement is in CountMaskedOpenCheckIns'
-- header -- the two statements share the next-record lookup and were measured together.
SELECT
    t.employee_id                                                  AS employee_id,
    e.full_name                                                    AS employee_name,
    l.name                                                         AS location_name,
    t.site_location_id                                             AS site_location_id,
    t.occurred_at                                                  AS occurred_at,
    t.channel = 'manual'                                           AS manager_typed,
    EXISTS (
        SELECT 1
        FROM transactions p
        WHERE p.tenant_id = @tenant_id
          AND p.employee_id = t.employee_id
          AND p.practice
          AND p.occurred_at > t.occurred_at
    )                                                              AS was_maskable
FROM transactions t
LEFT JOIN LATERAL (
    SELECT n.type, n.occurred_at, n.business_day_start
    FROM transactions n
    WHERE n.tenant_id = @tenant_id
      AND n.employee_id = t.employee_id
      AND n.type IS NOT NULL
      AND NOT n.practice
      AND (n.occurred_at, n.id) > (t.occurred_at, t.id)
    ORDER BY n.occurred_at, n.id
    LIMIT 1
) nx ON true
LEFT JOIN employees e
       ON e.id = t.employee_id
      AND e.tenant_id = t.tenant_id
LEFT JOIN locations l
       ON l.id = t.location_id
      AND l.tenant_id = t.tenant_id
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND t.type = 'in'
  AND NOT t.practice
  AND (nx.type IS DISTINCT FROM 'out'
       OR public.tappa_checkout_class(t.occurred_at, false, false, false, nx.occurred_at, nx.business_day_start)
          IS DISTINCT FROM 'closes')
ORDER BY t.occurred_at, t.id
LIMIT @row_limit;

-- name: CountMaskedOpenCheckIns :one
-- The check-ins a practice row could have masked, split by whether they are still open.
--
-- 🔴 IT WAS THE HALF THE OPEN LIST COULD NOT SHOW, AND SINCE M15-05 THE LIST SHOWS IT. Until
-- then "open" was `NOT EXISTS (an out after it)`, which a SINGLE later `out` satisfied for
-- every older unclosed check-in at once -- so a check-in a pre-ADR-0008 practice row had
-- masked left the list silently on the person's next checkout, and this second count was the
-- only place it was still visible. ListOpenCheckIns now reads ADR 0032 §A.6's definition: an
-- entry stays open unless its OWN next record closes it, and the record a mask produced is an
-- `in`, which closes nothing. So the masked entries are ON the list (with the "training tap
-- follows" hint), and the two numbers here are now:
--
--   still_open             a practice row follows it and nothing closes it -- the ones the
--                          list shows (uncapped, which the list is not).
--   closed_by_its_checkout a practice row follows it, and its own next record is a checkout
--                          that closes it (class 'closes'): an ordinary pair -- a training
--                          tap after it did not hide it.
--
-- The open/closed test is ListOpenCheckIns' own, written out AGAIN here (sqlc shares no SQL between
-- two statements): the same next-record LATERAL, the same class test. Inside this statement it is
-- written once, so the two counts cannot disagree with each other; that it agrees with the list's
-- copy is a test's claim, not the text's -- TestOpenCheckIns_TheListIsTheReportsOpenSet pins the
-- list, TestCountMaskedOpenCheckIns_SplitsByTheOneClassification this copy (a training row between
-- an in and its out included).
--
-- ⚠️ IT IS A HINT, NOT A DIAGNOSIS. "A practice row of this person's exists after this
-- check-in" is the shape the defect produced; it is also the shape an ordinary new starter
-- produces if they ever tap again after training. The screen calls it what it is.
--
-- ⚠️ AND THE DEVELOPMENT DATABASE WILL ALWAYS HOLD SOME. `chainFixture` leaves rows of this
-- shape on every `make test` run and section 4.3 plus `REVOKE DELETE` mean nothing can clear
-- them. There is no dev-only special case anywhere in this path -- the number says what it
-- counts and the reader decides.
--
-- ⚠️ THIS IS NOT A SECTION 4.6 BREACH: no row is lost -- `transactions` holds every one of them
-- and the transactions section lists them.
--
-- EXPLAIN (ANALYZE, BUFFERS), M15-05, 2026-10-21, the M15 development database after ANALYZE,
-- seed tenant 10000000-...-0001 (418 rows in the tenant, 262 of them directed and not practice),
-- the week from Monday 2026-10-19 00:00 Malta, three runs of each statement, before -> after the
-- next-record lookup:
--   ListOpenCheckIns (row_limit 51)
--     before  Index Scan Backward on transactions_tenant_occurred_idx (109 check-ins) -> Nested
--             Loop Anti Join probing transactions_tenant_employee_occurred_idx for a later `out`;
--             1.72 / 1.17 / 1.37 ms, shared hit 758 / 749 / 749
--     after   the same outer scan -> Nested Loop Left Join over a Limit 1 -> Incremental Sort ->
--             Index Scan Backward on transactions_tenant_employee_occurred_idx (109 probes) ->
--             the class as a join filter (57 removed, 52 kept, 51 returned);
--             2.61 / 1.61 / 1.68 ms, shared hit 975 / 939 / 939
--   CountMaskedOpenCheckIns
--     before  Hash Semi Join (9 check-ins with a training tap after them) + two anti-join probes;
--             0.85 / 1.23 / 1.03 ms, shared hit 269
--     after   the same semi join + one Limit 1 probe of the same index per check-in;
--             0.89 / 0.77 / 0.62 ms, shared hit 254
-- One observation on a small table, not a property: the lookup costs one index probe per
-- check-in of the window, as the anti-join did. An OLD week of a LARGE tenant is the exception the
-- M15-05 review measured (internal/handler/anomalies.go states it): neither the lookup nor
-- was_maskable is bounded by the week.
SELECT
    count(*) FILTER (WHERE s.open)                                 AS still_open,
    count(*) FILTER (WHERE NOT s.open)                             AS closed_by_its_checkout
FROM transactions t
LEFT JOIN LATERAL (
    SELECT n.type, n.occurred_at, n.business_day_start
    FROM transactions n
    WHERE n.tenant_id = @tenant_id
      AND n.employee_id = t.employee_id
      AND n.type IS NOT NULL
      AND NOT n.practice
      AND (n.occurred_at, n.id) > (t.occurred_at, t.id)
    ORDER BY n.occurred_at, n.id
    LIMIT 1
) nx ON true
CROSS JOIN LATERAL (
    SELECT (nx.type IS DISTINCT FROM 'out'
            OR public.tappa_checkout_class(t.occurred_at, false, false, false, nx.occurred_at, nx.business_day_start)
               IS DISTINCT FROM 'closes') AS open
) s
WHERE t.tenant_id = @tenant_id
  AND t.occurred_at >= @from_at
  AND t.occurred_at < @to_at
  AND t.type = 'in'
  AND NOT t.practice
  AND EXISTS (
      SELECT 1
      FROM transactions p
      WHERE p.tenant_id = @tenant_id
        AND p.employee_id = t.employee_id
        AND p.practice
        AND p.occurred_at > t.occurred_at
  );

-- name: ListCheckoutsWithoutCheckIn :many
-- Checkouts in the window with no open check-in for them to close -- the person's previous
-- record is a checkout (not a correction's surplus), a check-in of an earlier business day, or
-- nothing: the "check-in missing" list (M15-06; ADR 0032 §A.6, classes (b) other_day and (c) orphan).
--
-- 🔴 THE CLASS IS tappa_checkout_class's (migration 00064), THE ONE SQL FORM OF THE
-- CLASSIFICATION, fed the way ADR 0032 §A.6 defines its three inputs -- the same subqueries the
-- Go/SQL parity test feeds it (internal/domain/ledger's checkout parity test):
--
--   P  the person's directed, non-practice record just BEFORE the checkout, in (occurred_at, id)
--      order -- an `in` or an `out` (the LATERAL `pv`);
--   I  an `in` before it in that order with occurred_at >= its business day (the EXISTS);
--   W  P was written after it: pv.created_at > o.created_at. created_at is the DATABASE's write
--      instant (DEFAULT now(), which no statement writes -- TestTransactions_NoStatementWritesCreatedAt),
--      never occurred_at, the client's claim: a manager's correction typed into the past is
--      written later, a break's checkout tapped before is written earlier.
--
-- 🔴 WHAT IS NOT ON THIS LIST, AND WHY EACH IS LEFT OUT:
--   (e) extra       P is a checkout written LATER and the day has a check-in (I and W): a
--                   correction's surplus (ADR 0011's "out too late"). Listing it would leave a
--                   permanent "check-in missing" behind every such correction.
--   (d) unanchored  a row with NO business day (business_day_start IS NULL: every row before
--                   M15, every manual row). It cannot be judged against a day it does not carry;
--                   the predicate below says so explicitly as well as through the class.
--   (a), closes     P is a check-in this checkout consumes; (a)'s check-in is on the open list.
-- The report counts the same two classes (Totals.OutWithoutIn), except at its period seam -- a
-- business day that began before the week is the report's StartedEarlier; this list asks the
-- row question and lists it (ADR 0032 §A.6: "Anomali listeleri dönem değil satır sorusu sorar").
--
-- after_earlier_day is (b): the record before it is a check-in of an EARLIER business day -- that
-- check-in is on the open list, and the two rows are one forgotten tap or two. Otherwise (c):
-- nothing, or a checkout tapped before it, is before it.
--
-- A record naming no employee is not here: it has no chain (the report's Unattributable).
-- `NOT o.practice` and `NOT pv.practice` as in ListOpenCheckIns. site_location_id names the venue
-- of a checkout taken at a holding PEER's venue (location_id NULL -- K-30), named by the ledger
-- from holding_site_locations as the open list's is. Nothing read here is evidence (no address, no
-- distance, no verdict): who, where, when, and which of the two classes.
--
-- ORDERED OLDEST FIRST, the window on the CHECKOUT's occurred_at, LIMIT @row_limit -- the open
-- list's shape.
--
-- EXPLAIN (ANALYZE, BUFFERS), M15-06, 2026-10-22, the M15 development database, seed tenant
-- 10000000-0000-4000-8000-000000000001 (918 rows; the week from Monday 2026-10-19 00:00 Malta
-- holds 193 checkouts, 116 of them with a business day), row_limit 51, three runs: Bitmap Heap
-- Scan on transactions_tenant_occurred_idx (116 kept, 802 removed by the out / business-day
-- predicates) -> per checkout one Limit 1 probe of transactions_tenant_employee_occurred_idx for P
-- and one for I -> the class as a join filter (116 removed, 0 returned); 4.46 / 3.25 / 3.01 ms,
-- shared hit 1 891 / 1 669 / 1 669. The zero is M15-05's Phase A property seen on live rows: every
-- engine checkout is 'closes' or 'extra'. One observation on a small table, not a property: the
-- cost is two index probes per checkout of the week that carries a business day.
SELECT
    o.employee_id                                                  AS employee_id,
    e.full_name                                                    AS employee_name,
    l.name                                                         AS location_name,
    o.site_location_id                                             AS site_location_id,
    o.occurred_at                                                  AS occurred_at,
    (c.class = 'other_day')::boolean                               AS after_earlier_day
FROM transactions o
LEFT JOIN LATERAL (
    SELECT pv.type, pv.occurred_at, pv.created_at
    FROM transactions pv
    WHERE pv.tenant_id = @tenant_id
      AND pv.employee_id = o.employee_id
      AND pv.type IS NOT NULL
      AND NOT pv.practice
      AND (pv.occurred_at, pv.id) < (o.occurred_at, o.id)
    ORDER BY pv.occurred_at DESC, pv.id DESC
    LIMIT 1
) p ON true
CROSS JOIN LATERAL (
    SELECT public.tappa_checkout_class(
        CASE WHEN p.type = 'in' THEN p.occurred_at END,
        p.type = 'out',
        EXISTS (
            SELECT 1
            FROM transactions i
            WHERE i.tenant_id = @tenant_id
              AND i.employee_id = o.employee_id
              AND i.type = 'in'
              AND NOT i.practice
              AND (i.occurred_at, i.id) < (o.occurred_at, o.id)
              AND i.occurred_at >= o.business_day_start
        ),
        p.created_at > o.created_at,
        o.occurred_at,
        o.business_day_start) AS class
) c
LEFT JOIN employees e
       ON e.id = o.employee_id
      AND e.tenant_id = o.tenant_id
LEFT JOIN locations l
       ON l.id = o.location_id
      AND l.tenant_id = o.tenant_id
WHERE o.tenant_id = @tenant_id
  AND o.occurred_at >= @from_at
  AND o.occurred_at < @to_at
  AND o.type = 'out'
  AND NOT o.practice
  AND o.employee_id IS NOT NULL
  AND o.business_day_start IS NOT NULL
  AND c.class IN ('other_day', 'orphan')
ORDER BY o.occurred_at, o.id
LIMIT @row_limit;
