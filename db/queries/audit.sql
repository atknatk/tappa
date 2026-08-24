-- audit.sql -- the append-only administrative/domain trail (migration 00005).
--
-- WHY THIS FILE EXISTS NOW (M5-02 phase B): CLAUDE.md section 4.6 says a record is
-- never lost -- an attempt that fails must leave a trace rather than disappearing.
-- The activation endpoint is the first code path that produces failures worth
-- keeping (a replayed code, an expired one, a rate-limited caller, a second device
-- taking over an account), so it is the first caller that needs a writer. Until
-- now audit_log had a table and no INSERT query.
--
-- APPEND-ONLY IS ENFORCED BY THE DATABASE, NOT BY THIS FILE (00005): tappa_app
-- holds SELECT and INSERT only (REVOKE UPDATE, DELETE) AND a BEFORE UPDATE OR
-- DELETE trigger stops even tappa_owner. So there is deliberately no update and no
-- delete query here, and adding one would fail at runtime rather than quietly
-- rewriting history.
--
-- TENANT SCOPE (section 4.5): the INSERT names tenant_id explicitly as a VALUE and
-- runs inside db.(*DB).WithTenant, so the RLS WITH CHECK refuses a row whose
-- tenant disagrees with the transaction context (the same shape CreateInvite
-- uses -- there is no row to filter yet).
--
-- ⚠️ THE TENANT IS THE LIMIT OF THIS TRAIL, AND IT IS A REAL ONE. audit_log.tenant_id
-- is NOT NULL with an FK to tenants, so an event that cannot be attributed to a
-- tenant CANNOT be written here -- the clearest case being an activation attempt
-- with a code that resolves to nothing at all. There is no "system tenant" row and
-- inventing one would be worse than the gap: it would put unattributable events
-- inside a tenant's audit view. Those attempts are counted by the endpoint's rate
-- limiter and logged (without the code, section 4.7); the honest statement is that
-- the audit trail covers every failure whose OWNER is known, not every failure.
--
-- KIRMIZI CIZGI section 4.7 -- WHAT MUST NEVER GO IN `detail`: the invite code, its
-- hash, a session token, a CMAC, an AES key, or a full GPS coordinate. 00005 states
-- this and calls it the application's responsibility, because a jsonb column cannot
-- inspect its own contents. The Go side keeps it: internal/audit builds the detail
-- from a typed struct per event, never from a map of whatever the caller had.

-- name: RecordAuditEvent :one
-- Appends one event. Every column except the two nullable ones is supplied:
--
--   actor_id  NULL when the actor is the SYSTEM (00005: the column is
--             deliberately polymorphic and FK-less). An employee activating
--             themselves is NOT an admin actor, so activation events carry NULL
--             and name the employee in `target` instead.
--   target    the affected entity as opaque text -- an employee id for activation
--             events. Nullable because some events have no single target.
--
-- `at` is left to its DEFAULT now(): the event time is the DATABASE's, not a
-- caller-supplied timestamp, so a clock-skewed or malicious caller cannot
-- backdate the trail (the same reason transactions.created_at is server-side).
--
-- RETURNING id, at: the caller logs the id (a stable, non-secret handle) instead
-- of the payload, and `at` lets a test assert the row exists without re-reading.
INSERT INTO audit_log (tenant_id, actor_id, action, target, detail)
VALUES (@tenant_id, @actor_id, @action, @target, @detail)
RETURNING id, at;

-- name: ConfirmRecentRemoval :one
-- Did THIS admin, in THIS business, remove THIS row, JUST NOW?
--
-- 🔴 IT EXISTS SO THE PANEL CAN ACKNOWLEDGE A DELETION WITHOUT LYING (user decision,
-- 2026-08-09, reading C'). Every other panel banner survives a replayed URL because
-- the row it describes is rendered underneath it; a DELETED row cannot be, so a
-- `?done=venue-deleted` word was measured printing "The venue has been removed" in a
-- browser that had never posted anything. M6-05's rule for an action claim is that the
-- handler VERIFIES it against a row the same request read -- and after a delete the
-- only surviving row is the audit entry. This is that row.
--
-- 🔴 IT IS NOT AN AUTHORISATION AND MUST NEVER BECOME ONE. Nothing is permitted on the
-- strength of this answer; it decides whether a SENTENCE is printed. That is why a
-- failure to read it renders the list without a banner rather than a problem page.
--
-- 🔴 AND IT MUST NOT BECOME AN ORACLE. Both the tenant AND the actor are bound, so a
-- manager cannot learn that some OTHER admin removed something, and no business can
-- learn anything about another. Four inputs collapse to the same empty answer:
-- another tenant's genuinely removed id, a colleague's removal inside this tenant, a
-- uuid that never existed, and a well-formed uuid for a row never deleted.
--
-- 🔴 @actor_id AND @target ARE CAST EXPLICITLY, AND BOTH CASTS ARE LOAD-BEARING.
-- audit_log.actor_id is NULLABLE by schema decision (00005: the column is polymorphic
-- and holds NULL for SYSTEM events) and target is nullable too. Without the casts sqlc
-- infers both PARAMETERS as nullable and emits *uuid.UUID / *string -- so a nil actor
-- would match every SYSTEM-generated row and a nil target every row with no single
-- subject, printing an acknowledgement for something the signed-in manager did not do.
-- Measured: sqlc v1.28 emitted exactly those pointer types before the casts were
-- added. Same defect CountDepartmentReferences hit on a nullable department_id, which
-- is why it is written out again rather than assumed learned.
--
-- THE WINDOW IS THE DATABASE'S OWN CLOCK. `now()` is evaluated server-side inside the
-- statement and nothing a client sends reaches it -- ADR 0006's rule satisfied
-- structurally rather than by discipline. make_interval keeps the bound a parameter so
-- one Go constant is its single source.
--
-- RETURNS THE NAME OUT OF THE TRAIL, which is the only place it still exists: the row
-- itself is gone and the id in the address bar is a CLIENT value, so a heading built
-- from it would be a sentence the client wrote.
--
-- ⚠️ coalesce(..., '') RATHER THAN A BARE ->>, AND THAT IS ABOUT THE SCAN. sqlc cannot
-- type a jsonb `->>` at all (it emitted `interface{}`), and casting alone gives a
-- NON-nullable Go string that would FAIL to scan the moment a trail row had no `name`
-- key -- a future action with a different detail shape, or an older row. Every
-- location.deleted row this product writes carries one, so today it is defensive; the
-- empty string then means "the trail has no name for it" and the heading falls back.
SELECT coalesce((detail->>'name')::text, '')::text AS removed_name
FROM audit_log
WHERE tenant_id = @tenant_id
  AND actor_id = @actor_id::uuid
  AND action = @action
  AND target = @target::text
  AND at > now() - make_interval(secs => @window_seconds::int)
ORDER BY at DESC
LIMIT 1;

-- name: ListPlaqueHistory :many
-- WHO did WHAT to this plaque, and WHEN -- the audit half of M6-06's "tag history is
-- visible (audit)" criterion.
--
-- 🔴 IT EXISTS BECAUSE THE `tags` ROW CANNOT ANSWER THE MANAGER'S ACTUAL QUESTION.
-- The row carries created_at, retired_at and the replaced_by chain, so the screen can
-- already say WHAT happened and WHEN. It has no actor column and never will -- 00004
-- gave it none -- so "who took this plaque off the wall?" is answerable only from the
-- trail internal/domain/tenant writes with audit.RecordTx inside the write's own
-- transaction. A history that shows the change but not the person is the half that
-- matters least when something has gone wrong at a door.
--
-- 🔴 THE ACTION FILTER IS A PREFIX AND THAT IS DELIBERATE. `plaque.%` covers
-- whatever the next plaque act is called on the day it is added -- the same reason
-- venue.go's header gives for `action LIKE 'location.%'`. A hand-listed pair would
-- silently stop describing the product the moment a third act shipped.
--
-- ⚠️ AND THIS LINE USED TO NAME THE SET ("covers plaque.mounted and plaque.retired
-- today"), WHICH IS THE DEFECT ITS OWN NEXT SENTENCE WARNS ABOUT, IN PROSE INSTEAD
-- OF IN SQL. It went stale twice: plaque.unmounted shipped in the same change set
-- that wrote it, and M8-05 FAZ B2c-2a added plaque.loaded and plaque.encoded
-- (internal/encode, ADR 0017 §5.1 steps 3 and 9). The number is deliberately not
-- restated here. Where the set IS counted is internal/handler's
-- TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite, which derives it from
-- internal/domain/tenant's const block with go/ast rather than from a sentence.
--
-- 🔴 @target IS CAST EXPLICITLY, for ConfirmRecentRemoval's measured reason:
-- audit_log.target is nullable by schema (00005), so without the cast sqlc infers the
-- PARAMETER as nullable and emits *string -- and a nil would then match every row
-- with no single subject, i.e. this plaque's history would include acts on nothing.
--
-- 🔴 THE ACTOR'S NAME IS JOINED, NOT STORED IN `detail`. A name in the trail row
-- would be the name at the time of writing and would drift from the person; the join
-- answers "who is this now", which is what a manager chasing a door needs. LEFT JOIN
-- because actor_id is nullable (00005: the column is polymorphic and holds NULL for
-- SYSTEM events) -- a system-written act must still appear, with no name rather than
-- with no row. Both sides of the join carry @tenant_id: section 4.5's belt asks the
-- SUBJECT's scope column to be bound, and binding the joined table's as well means a
-- future edit cannot turn this into a cross-tenant name lookup.
--
-- coalesce(..., '')::text RATHER THAN A BARE COLUMN, and it is about the scan: a
-- LEFT JOIN's right side is NULL for a SYSTEM event, and a non-nullable Go string
-- would fail to scan on exactly the row this query must not drop.
--
-- 🔴 by_system IS A SEPARATE COLUMN BECAUSE AN EMPTY NAME HAS TWO CAUSES AND THEY ARE
-- DIFFERENT SENTENCES. admin_users.full_name is `text NOT NULL` with NO non-blank
-- CHECK, so '' is a storable name; actor_id is nullable and holds NULL for SYSTEM
-- events. Reading "" as "the system did it" would attribute a NAMELESS ADMIN'S act to
-- the product — an attribution error in the one place a manager goes to ask who
-- changed the plaque on their door. The screen now says "by the system" only when the
-- ROW says so.
--
-- 🔴 @row_limit BOUNDS THE OUTPUT, NOT THE WORK, AND THE DIFFERENCE IS THE WHOLE
-- COST OF THIS QUERY. The SCAN is over every audit row the TENANT has, and
-- audit_log is append-only with no retention job (backlog T6/T13), so the cost
-- grows with the business's age rather than with the plaque's.
--
-- ⚠️ THIS PARAGRAPH USED TO OPEN "A plaque's history is THREE ROWS AT MOST" AND
-- THAT CEILING IS GONE (M8-05 FAZ B2c-2a, 2026-08-24) -- a closed count, forty
-- lines under a paragraph the same change set was editing. Two things broke it:
-- the vocabulary went from three acts to FIVE (plaque.loaded and plaque.encoded,
-- ADR 0017 §5.1 steps 3 and 9), and plaque.encoded is NOT capped at one -- the
-- marker is idempotent on the ROW but audit_log is append-only, so a RETRIED
-- marking appends another true entry. internal/encode's own test drives exactly
-- that ("plaque.encoded entries = 2").
--
-- 🔴 WHAT THAT COSTS, NAMED RATHER THAN RE-COUNTED: plaqueHistoryLimit = 20
-- (internal/domain/tenant/plaque.go) justified itself as "far above any real
-- chain", and THAT JUSTIFICATION NO LONGER HOLDS -- a chain is now unbounded in
-- principle, because a caller that retries a marking N times writes N rows. No new
-- number is put here: a ceiling is exactly what went stale, and the limit's real
-- job (bounding what crosses the wire and what a template renders) never depended
-- on one. ⚠️ NOT REACHABLE TODAY -- a second round against one chip dies at the
-- duplicate uid, and the endpoint that could retry a marking is FAZ B2c-2b's.
--
-- MEASURED 2026-08-10, with its population as this repo's rule requires -- tenant
-- 10000000-...-0001, 2 566 audit rows of 63 624 in a 19 MB relation, asked for a
-- plaque with NO history (the worst case, because nothing short-circuits):
--
--   Bitmap Index Scan on audit_log_tenant_at_idx (2 566 rows, 25 buffers)
--     -> Bitmap Heap Scan on audit_log, Rows Removed by Filter: 2 566,
--        Heap Blocks: exact=1 039, Buffers: shared hit=1 064
--   Execution Time: 4.174 ms, rows returned: 0
--
-- So the tenant predicate is indexed and the `target`/`action` predicates are not:
-- every row of the tenant is fetched and thrown away. At 2 566 rows that is ~4 ms and
-- invisible; the shape is LINEAR in the tenant's audit history, and a manager may
-- open adminSessionLimit (300 per 10 minutes) cards.
--
-- ⚠️ NO INDEX IS ADDED HERE, deliberately: an index is a MIGRATION, 00013 was this
-- phase's slot and it is spent (CLAUDE.md section 6 -- an applied migration is not
-- edited). The candidate is (tenant_id, target) or (tenant_id, action, target); it is
-- recorded in the backlog rather than smuggled in, and M6-07's reports will want
-- audit indexes too, so the two are cheaper measured together.
--
-- THE LIMIT STAYS ANYWAY. It bounds what crosses the wire and what a template
-- renders, which is the half a row cap can actually give -- an unbounded read is what
-- M6-03 measured at 867 KB and removed.
SELECT a.action, a.at, coalesce(u.full_name, '')::text AS actor_name,
       (a.actor_id IS NULL)::boolean AS by_system
FROM audit_log a
LEFT JOIN admin_users u ON u.id = a.actor_id AND u.tenant_id = @tenant_id
WHERE a.tenant_id = @tenant_id
  AND a.target = @target::text
  AND a.action LIKE 'plaque.%'
ORDER BY a.at DESC
LIMIT @row_limit::int;
