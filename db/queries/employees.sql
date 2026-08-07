-- employees.sql -- employee reads. NO WRITE QUERY LIVES HERE, and that absence is
-- load-bearing: db/queries/invites.sql documents that ConsumeInviteAndActivate is
-- the ONLY statement in db/queries that writes employees.status, so that
-- "activated" implies "an invite was spent". Adding an activate/deactivate query
-- to this file would silently break that property (M5-02 phase A, binding
-- decision 2). Deactivation (M6-05) gets its own query with its own justification.
--
-- TENANT SCOPE (CLAUDE.md section 4.5, belt + braces on RLS): every query here
-- carries an explicit tenant_id predicate and runs inside db.(*DB).WithTenant.
-- The tenant comes from the context-less resolver (ADR 0002 madde 7), never from
-- the client.

-- name: GetEmployeeActivationContext :one
-- Everything the activation page must render about WHO is activating, in one
-- round trip: the greeting name, the current lifecycle status, the location whose
-- WiFi step comes next, and the DATA CONTROLLER's name for the GDPR Art. 13
-- notice.
--
-- WHY status IS SELECTED. Two callers need it and neither is cosmetic:
--   * the page refuses to invite a 'deactivated' person to activate (the
--     consuming statement would refuse anyway -- this only avoids showing a form
--     that cannot succeed);
--   * the POST path reads it BEFORE consuming to tell a FIRST activation from a
--     SECOND one (a new phone). That read is not a security decision -- the
--     consumption is atomic and authoritative -- it only decides whether the
--     employee's older sessions get revoked. A race there revokes sessions that
--     did not need revoking, which is fail-closed.
--
-- WHY THE TENANT NAME IS JOINED RATHER THAN QUERIED SEPARATELY. GDPR Art. 13(1)(a)
-- requires the identity of the CONTROLLER, and the controller here is the
-- employer, not Tappa (docs/handoff.md). Naming them costs one join on a row the
-- transaction can already see; a second query would cost a second round trip and
-- a second place to forget the tenant filter. The join is safe under RLS: the
-- tenants policy scopes on id, and inside WithTenant that is the same tenant.
--
-- NOT RETURNED: email, role, invited_at, deactivated_at, created_at. The
-- activation page has no use for them and the resolver-function precedent
-- (00003/00004/00009) is "no more columns than the caller needs".
--
-- THIRD CALLER (M5-04): the TAP PAGE reads full_name here for its greeting
-- (internal/domain/tenant.Directory.TapPage). It uses no other column -- the
-- venue it names is the TAPPED location, not e.location_id, and the status is
-- deliberately not a page input: whether a deactivated employee's tap is refused
-- is the sys:employee-deactivated guardrail's answer at POST time, and §5 row 4
-- requires that refusal to be RECORDED, which a page cannot do. The name is
-- kept as-is rather than widened to "GetEmployee...": one query serving the two
-- screens that greet somebody is the point, and a second near-identical query
-- would be the drift this file's header warns about.
SELECT e.id, e.full_name, e.status, e.location_id, t.name AS tenant_name
FROM employees e
JOIN tenants t ON t.id = e.tenant_id
WHERE e.tenant_id = @tenant_id
  AND e.id = @employee_id;

-- name: GetEmployeeForTap :one
-- Everything the DECISION engine needs to know about the person tapping
-- (M5-05). It is a SECOND read of the same table rather than a widening of
-- GetEmployeeActivationContext above, and the reason is that the two answer
-- different questions with different audiences: that one builds a page (a name,
-- an employer, a venue) and this one builds tap.Input (a lifecycle state, two
-- scope ids, an activation instant). Widening the first would have put the
-- greeting query in the position of also being the decision query, and every
-- column added for one purpose would then be rendered by the other.
--
-- WHAT EACH COLUMN DECIDES, so nothing here is decoration:
--   * status         -> sys:employee-deactivated (§5 row 4: reject, RECORD it,
--                       and raise a security alert). The tap page deliberately
--                       does not read this; a page cannot record a refusal.
--   * location_id    -> the employee's HOME location, compared against the
--                       TAPPED one for the cross-location fact (Q17). It never
--                       penalises a tap — chain staff move between branches —
--                       and the evidence is always matched against the TAPPED
--                       location, never this one.
--   * department_id  -> which shift applies: the department's if there is one,
--                       otherwise the tapped location's (§5, M4-05). Nullable.
--   * activated_at   -> the practice (TRAINING) tap: the first record after
--                       activation never counts toward hours. SERVER-side, which
--                       is what stops a client claiming practice=true on a real
--                       checkout to keep it out of the totals (M4-06).
--   * timezone       -> the tenant's IANA zone (Q01), needed to turn a shift's
--                       WALL-CLOCK start into an instant so lateness survives the
--                       Malta DST transitions. Joined for the same reason the
--                       query above joins the tenant name: one round trip, one
--                       place to remember the tenant filter.
--
-- TWO OF THE COLUMNS ALSO REACH THE RENDER LAYER, and that is stated rather than
-- left to be discovered, because an earlier version of this block described the
-- query as feeding tap.Input and nothing else:
--   * timezone       is BOTH a decision input (above) and what turns the stored
--                    UTC instant into the wall clock on the confirmation screen.
--   * business_type  is RENDER ONLY (M5-06). Nothing in tap.Input carries it and
--                    no rule reads it; it selects which of the fixed brand
--                    sentences the confirmation screen ends with ("Have a great
--                    shift - keep those kebabs rolling!" for a restaurant, the
--                    factory-floor wording for production, a neutral one for
--                    every other type). It is a CATEGORY, not an identity: the
--                    column is a CHECK-constrained enum of eight generic values
--                    (00001), so no tenant name, id or other tenant's data
--                    travels with it. Per-TENANT editable copy is M9-04's job and
--                    needs a column that does not exist yet -- until then two
--                    restaurants get the same sentence, which is a known limit
--                    and not a bug in this query.
--
-- NOT RETURNED: full_name, email, role, invited_at, deactivated_at. A DECISION
-- never reads a name, and a record that cannot hold one cannot leak one. That
-- claim is about the four columns above, not about the whole result set: the two
-- presentation columns exist precisely because the screen that follows the
-- decision has to say something, and neither of them is a name.
SELECT e.status, e.location_id, e.department_id, e.activated_at,
       t.timezone AS tenant_timezone, t.business_type AS tenant_business_type
FROM employees e
JOIN tenants t ON t.id = e.tenant_id
WHERE e.tenant_id = @tenant_id
  AND e.id = @employee_id;

-- name: ListPanelEmployees :many
-- The PANEL's ROSTER read (M6-05 phase A): one page of the people who work here,
-- alphabetically, with the two facts the card asks for beside each name -- where
-- they work, where they are in the employment lifecycle, and whether a phone of
-- theirs is still signed in.
--
-- 🔴 IT LISTS THE DEACTIVATED BY DEFAULT AND THERE IS NO STATUS PREDICATE UNLESS
-- THE CALLER ASKS FOR ONE (section 4.6). Somebody who has left is exactly the
-- person a manager comes looking for months later -- a payroll query, a dispute, a
-- reference -- and a list that quietly drops them makes their records unreachable
-- through the only screen that answers "who works here?". M6-03 rejected three
-- shortlisting options for this reason and handed the question to this section;
-- the same rule applies here. The status parameter NARROWS on request and defaults
-- to NULL, which matches every row.
--
-- 🔴 SECTION 4.7 -- WHAT THIS QUERY DELIBERATELY DOES NOT SELECT. The tables it
-- reads carry secrets and near-secrets, and none of them is in the column list:
--
--	sessions.token_hash   the HMAC of a live credential. It exists to be MATCHED
--	                      (db/queries/sessions.sql says so) and is never handed
--	                      back to Go, here least of all -- this row would put it on
--	                      a rendered page.
--	sessions.device_info  a coarse device label ("iPhone Safari"). Not a secret,
--	                      but nothing on this screen needs it: the card asks
--	                      whether an ACTIVE DEVICE EXISTS and when it was last
--	                      used, and a count plus a timestamp answer both. A column
--	                      selected for no reader is the thing that weakens the
--	                      argument above, so it stays out (the same finding that
--	                      removed four columns from ListPanelTransactions).
--	sessions.id           an identifier for a credential. The panel has no per-
--	                      session control in phase A, so there is nothing to name.
--	employees.email       the invitation address. Phase B (invite / re-invite) is
--	                      where an address earns a reader; a roster does not.
--	employee_invites.*    NOT JOINED AT ALL. The invite CODE is hashed
--	                      (code_hash) and the card's "shown exactly once" rule is
--	                      phase B's; a list that touched the invite table would be
--	                      one edit away from rendering one.
--
-- WHAT IT DOES SELECT ABOUT SESSIONS IS TWO AGGREGATES, and they are computed in a
-- LATERAL rather than per row in Go. ListSessionsForEmployee already exists and
-- answers this for ONE person; calling it once per listed employee is an N+1.
--
-- 🔴 THE TABLE BELOW WAS RE-MEASURED AT THE SHIPPED PAGE SIZE AND THE FIRST VERSION
-- OF IT WAS WRONG IN THE FLATTERING DIRECTION. It quoted "a page of 26", which is
-- RosterPageSize+1 for the page size this section had BEFORE the 2026-08-07
-- decision -- so the numbers described a query the product no longer runs, and they
-- understated the lateral's cost. Re-measured at LIMIT 51 (50 + the one extra row),
-- largest tenant, warm cache, ANALYZEd, 5 repeats each:
--
--	page of 51 WITHOUT the lateral (baseline)       3.7 - 9.2 ms
--	page of 51 WITH the lateral (SHIPPED)          16.1 - 18.5 ms
--	ONE ListSessionsForEmployee call                0.7 - 1.2 ms
--	  => the N+1 shape, 50 rows                    35 - 58 ms  AND 50 extra round
--	                                                            trips on a pooled
--	                                                            connection shared
--	                                                            with the tap path
--
-- 🔴 AND THE MECHANISM IS NOT WHAT "26 INDEX PROBES" IMPLIED. EXPLAIN ANALYZE shows
-- the probes themselves are nearly free -- loops=51 on an Index Scan using
-- sessions_tenant_idx, well under a millisecond in total. What the lateral actually
-- costs is a PLAN CHANGE above it: without it the sort is a `top-N heapsort` that
-- stops after 51 rows and needs tens of kilobytes; with it the planner sorts the
-- tenant's WHOLE roster with a `quicksort` needing about a megabyte across its
-- workers. So the cost is roughly 2-4x the baseline and it scales with the ROSTER,
-- not with the page.
--
-- ⚠️ THE SORT'S MEMORY IS DELIBERATELY GIVEN AS AN ORDER RATHER THAN A FIGURE. It is
-- proportional to the roster, which is the magnitude that grows on every `make test`
-- run (see the drift warning in internal/handler/adminratelimit.go) -- an exact
-- kilobyte count here would be stale on the same schedule as the headcount it
-- derives from, which is the defect three audits of this task kept finding.
--
-- THE DECISION IS UNCHANGED AND THE REASON IS UNCHANGED: one round trip at 16-18 ms
-- beats fifty round trips carrying 35-58 ms of database time. What changed is the
-- margin, which was overstated. No index is added and none is needed --
-- sessions_tenant_idx (tenant_id, employee_id) is migration 00003's and it is what
-- the probe uses.
--
-- ⚠️ THE AGGREGATES ARE OVER LIVE SESSIONS ONLY, and that is a choice with a cost.
-- revoked_at IS NULL is inside the lateral, so last_used_at is the last use of a
-- device that is STILL signed in. A person whose phone was revoked yesterday reads
-- as "no active device" with no date beside it. That is the honest answer to the
-- card's question ("aktif cihaz var mi, son kullanim") and it is deliberately not
-- a device HISTORY: revoked rows are never deleted (00003 REVOKEs DELETE on
-- sessions) and a history view would be its own query with its own screen.
--
-- KEYSET PAGINATION ON (full_name, id), NOT OFFSET. The pair is a total order --
-- id breaks ties between namesakes, and a roster has plenty -- so no person can be
-- skipped or shown twice when somebody is invited while a manager is halfway down
-- the list. It is client-supplied and that is safe for the same reason the
-- transactions cursor is: it can only move the reader within their OWN tenant,
-- because the tenant predicate is not a parameter the client contributes to.
--
-- 🔴 THE ORDER IS ALPHABETICAL BECAUSE THE QUESTION IS "WHO WORKS HERE?". Sorting
-- by created_at (the shape ListPanelTransactions uses) would be cheaper to reason
-- about and useless to browse: a manager looking for Maria does not know when she
-- was hired. Measured cost of the sort on the largest tenant: a top-N sort of the
-- whole roster, 6.1 - 13.3 ms thousands of names deep -- FLAT with depth, which is
-- what keyset buys, and the flatness is the finding rather than the milliseconds.
-- No index on (tenant_id, full_name) exists and none is added; adding one is a
-- migration and the sort does not justify it today.
--
-- TENANT SCOPE (section 4.5, belt + braces): the explicit e.tenant_id predicate is
-- required even though RLS is on, every JOIN restates tenant_id in its own ON
-- clause, and the LATERAL carries its own e.tenant_id-independent predicate bound
-- to the CALLER's parameter rather than to the outer row -- so a lateral that
-- somehow saw a foreign employees row still could not read that tenant's sessions.
--
-- ⚠️ OF THOSE THREE, ONLY TWO ARE NETTED; THE JOIN ... ON RESTATEMENT IS DISCIPLINE.
-- Measured rather than assumed: deleting `l.tenant_id = e.tenant_id` from the
-- locations JOIN below leaves internal/domain/ledger ok AND internal/handler ok --
-- the whole suite green. internal/domain/ledger/query_test.go excludes JOIN ... ON
-- structurally and correctly (a condition there constrains the join, not the rows
-- returned), which is exactly why nothing watches it. RLS still stops foreign rows;
-- what is unguarded is the second defence on the JOINs specifically. It is written
-- here rather than netted because the net that would catch it is the same widening
-- that file has now declined twice, with its reasons.
--
-- THE JOINS ARE LEFT JOINS: department_id is nullable by design (00003 -- a bar
-- does not model departments), and location_id is NOT NULL but a LEFT JOIN costs
-- nothing and cannot drop a person if the row is ever repaired badly. Dropping a
-- person from the roster because a join missed is a section 4.6 breach committed
-- by a read.
-- 🔴 THE SHAPE OF THE LATERAL IS DICTATED BY sqlc's NULLABILITY INFERENCE, AND THE
-- TWO OBVIOUS SPELLINGS BOTH PRODUCE A ROW TYPE THAT IS WRONG AT RUNTIME. Measured
-- with sqlc v1.28 by generating each one (the M1-08 lesson: a generator's exit code
-- is not a type check), and RE-MEASURED after an audit could not reproduce them from
-- the shipped file alone. To re-run: copy db/ and sqlc.yaml into an empty directory,
-- replace db/queries with the four forms below, and generate.
--
--	max(s.last_used_at)                 -> `SessionLastUsedAt interface{}`  sqlc
--	                                       cannot type an aggregate through a
--	                                       LATERAL at all
--	max(s.last_used_at)::timestamptz    -> `SessionLastUsedAt time.Time`    the cast
--	                                       makes it NOT NULL, and max() over zero
--	                                       live sessions IS NULL -- pgx would fail
--	                                       to scan for every person with no phone
--	                                       signed in, which is the ORDINARY case
--	count(*) OVER () (bare)             -> `LiveSessions int64` while the LEFT JOIN
--	                                       can yield NULL -- the same scan failure
--	                                       in the other column
--
-- What is written below types correctly BECAUSE of its shape rather than in spite
-- of it: last_used_at is a PLAIN COLUMN of a LEFT-JOINed subquery (so sqlc infers
-- *time.Time, which is what "nobody signed in" needs), and the count is COALESCEd
-- in the OUTER select (so it is never NULL and int64 is honest). COALESCE(...,0) is
-- not a fabricated value here: no lateral row means literally zero live sessions.
--
-- count(*) OVER () COUNTS BEFORE THE LIMIT. Window functions are evaluated after
-- WHERE and before ORDER BY/LIMIT, so the LIMIT 1 picks the most recently used
-- device while the count still sees every live one. Verified against ground truth
-- rather than argued: the same aggregates computed by a GROUP BY over the whole
-- largest tenant agree row for row -- 0 count mismatches and 0 timestamp mismatches,
-- re-run twice as that tenant grew. The load-bearing claim is THE TWO ZEROS; how many
-- rows were compared is a timestamp rather than a fact, because that fixture is hired
-- into by every `make test` run and employees cannot be deleted, so it is not written
-- down.
--
-- NULLS LAST is load-bearing: last_used_at is nullable (00003 -- it is stamped on
-- USE, and a session created seconds ago has never been used), so a freshly issued
-- device must not sort above one that has actually been used.
SELECT e.id, e.full_name, e.status, e.invited_at, e.activated_at, e.deactivated_at,
       l.name AS location_name,
       d.name AS department_name,
       COALESCE(ls.live_sessions, 0)::bigint AS live_sessions,
       ls.last_used_at AS session_last_used_at
FROM employees e
LEFT JOIN locations   l ON l.tenant_id = e.tenant_id AND l.id = e.location_id
LEFT JOIN departments d ON d.tenant_id = e.tenant_id AND d.id = e.department_id
LEFT JOIN LATERAL (
    SELECT s.last_used_at, count(*) OVER () AS live_sessions
    FROM sessions s
    WHERE s.tenant_id = @tenant_id
      AND s.employee_id = e.id
      AND s.revoked_at IS NULL
    ORDER BY s.last_used_at DESC NULLS LAST
    LIMIT 1
) ls ON TRUE
WHERE e.tenant_id = @tenant_id
  AND (sqlc.narg('status')::text IS NULL
       OR e.status = sqlc.narg('status')::text)
  AND (sqlc.narg('location_id')::uuid IS NULL
       OR e.location_id = sqlc.narg('location_id')::uuid)
  AND (sqlc.narg('department_id')::uuid IS NULL
       OR e.department_id = sqlc.narg('department_id')::uuid)
  AND (sqlc.narg('full_name')::text IS NULL
       OR e.full_name ILIKE '%' || sqlc.narg('full_name')::text || '%')
  AND (sqlc.narg('cursor_name')::text IS NULL
       OR (e.full_name, e.id) > (sqlc.narg('cursor_name')::text,
                                 sqlc.narg('cursor_id')::uuid))
ORDER BY e.full_name ASC, e.id ASC
LIMIT sqlc.arg('page_size')::int;

-- name: GetRosterCursorAnchor :one
-- The name of ONE employee, by id -- the anchor a roster page continues from.
--
-- 🔴 IT EXISTS SO THE PAGING CURSOR CAN CARRY AN ID INSTEAD OF A NAME, and that is a
-- section 4.6 fix rather than a tidy-up. The cursor used to carry the anchor's
-- full_name in the query string with a length bound on it, because a query string is
-- not a place to put an unbounded value. full_name IS unbounded (`text`, no CHECK),
-- so a name past the bound was dropped -- and a dropped cursor makes "next page"
-- serve the SAME page. Measured by a security audit on real Postgres: a 605-rune
-- name on a page boundary served page one forever, leaving everybody behind it
-- unreachable by browsing and listing the first page twice.
--
-- Reading the name here removes the failure at its source: an id is 36 characters
-- whatever the person is called, so there is nothing left to bound and nothing left
-- to drop.
--
-- IT ALSO TAKES A PERSON'S NAME OUT OF THE URL, which the Referrer-Policy note in
-- internal/handler/adminlogin.go had recorded as an open privacy wrinkle: a "next
-- page" link used to contain a real employee's full name, so it reached browser
-- history and anything looking at the address bar. That is closed as a side effect
-- rather than as the goal, and it is the reason this shape was chosen over an opaque
-- encoded token -- a token still carries the name, just illegibly, so it would have
-- fixed neither the length nor the disclosure.
--
-- COST: one lookup on the primary key, paid ONLY on a paged request (never on a
-- section view or a filter change). Measured through tappa_app on the largest
-- tenant, warm, 5 repeats: 0.44 - 0.51 ms, against a roster page that costs
-- 19 - 68 ms -- about 2%.
--
-- ⚠️ THAT FIGURE WAS WRITTEN HERE AS "0.6 - 1.9 ms" BEFORE IT WAS MEASURED, and the
-- correction is left visible because the mistake has a shape worth recognising: the
-- first timing bound a subquery that FOUND the id into the measurement, which is work
-- the product never does -- it already has the id, it came from the URL. Timing a
-- probe instead of the production shape is the M0-04 lesson, and it inflated this
-- number roughly fourfold.
--
-- TENANT SCOPE (section 4.5, belt + braces): the explicit tenant_id predicate is
-- required even though RLS is on. A foreign id returns no row, which the caller reads
-- as "no cursor" and answers with the first page -- more of the caller's OWN roster,
-- never somebody else's.
SELECT full_name
FROM employees
WHERE tenant_id = @tenant_id
  AND id = @id;
