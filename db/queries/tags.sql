-- tags.sql -- tenant-scoped tag queries. Runs AFTER GetTagByUID (resolve.sql)
-- has established the tenant context, so per CLAUDE.md section 4.5 every query
-- here carries an explicit tenant_id filter (belt + braces on top of RLS).

-- name: AdvanceTagCounter :one
-- THE replay guard (CLAUDE.md section 4.4, the single most critical query). The
-- counter advance is ONE atomic statement: the UPDATE succeeds only when the
-- presented ctr is strictly greater than the stored last_ctr. Comparison is '<',
-- NEVER '>=' -- '>=' would let the SAME counter pass twice, which is replay
-- itself. On replay (equal or smaller ctr) prev.old_ctr < @ctr is false, no row
-- updates, RETURNING is empty, and the caller sees pgx.ErrNoRows -> reject.
--
-- Returning the ctr gap (ctr - old_last_ctr - 1) for base:ctr-gap-review (M3-06)
-- is why we cannot use a bare UPDATE ... RETURNING: we need the OLD last_ctr, but
-- in PG16/17 RETURNING sees the NEW row. The prev CTE captures the OLD value
-- under the statement-start snapshot; the UPDATE joins it back via FROM. The gap
-- is then t.last_ctr - prev.old_ctr - 1: after SET, RETURNING's t.last_ctr IS the
-- new value (= @ctr), and prev.old_ctr is the pre-update value -- so no parameter
-- appears in the RETURNING expression (a param inside arithmetic in RETURNING
-- trips sqlc v1.28's query re-parse; t.last_ctr sidesteps it and is provably @ctr).
--
-- FOR UPDATE in prev locks the row so two concurrent taps with the same ctr
-- serialise: the second blocks, then re-reads last_ctr = @ctr under READ
-- COMMITTED, its prev.old_ctr < @ctr is false, and it updates 0 rows -> ErrNoRows.
-- Exactly one wins. (Basic 2-goroutine atomicity is probed in the store test; the
-- full N-goroutine -race proof belongs to M2-06, per the M1-08 brief.)
--
-- Every table reference is aliased and every column qualified: sqlc v1.28 leaks
-- the outer UPDATE target (tags t) into the prev CTE's scope, so a bare `uid`
-- inside prev is (wrongly) flagged ambiguous. Qualifying with p./t. and renaming
-- prev's outputs (old_uid/old_ctr) removes any bare `uid` and satisfies both
-- sqlc and Postgres.
WITH prev AS (
    SELECT p.uid AS old_uid, p.last_ctr AS old_ctr
    FROM tags p
    WHERE p.uid = @uid AND p.tenant_id = @tenant_id
    FOR UPDATE
)
UPDATE tags t
SET last_ctr = @ctr
FROM prev
WHERE t.uid = prev.old_uid
  AND t.tenant_id = @tenant_id
  AND prev.old_ctr < @ctr
RETURNING t.uid, (t.last_ctr - prev.old_ctr - 1)::integer AS ctr_gap;

-- ============================================================================
-- THE PANEL SIDE (M6-06 phase B, migration 00013). Everything below serves the
-- INVENTORY MODEL: Tappa encodes a plaque and LOADS the row, the panel only
-- BINDS it to a wall (user decision 2026-08-08).
--
-- 🔴 THERE IS NO INSERT HERE, AND THAT IS THE DECISION, NOT AN OMISSION. Creating
-- a plaque means holding its AES key, and the key never passes through the panel
-- (section 4.7, Q06: the key is produced by the M8-05 runbook). The row is loaded
-- by an operator as tappa_owner. The panel's whole vocabulary is: list, read, bind,
-- UN-bind and retire -- never create and never delete.
--
-- 🔴 AND aes_key_ref IS IN NO COLUMN LIST BELOW. It is a KEK-wrapped envelope,
-- useless without the KEK, but useless-if-stolen is not a reason to hand it to a
-- screen. The ONE path that legitimately needs it is the SUN verification, which
-- reads it through resolve_tag_by_uid (00004) and nowhere else. A panel query
-- that selected it would put a wrapped key into template data, log lines and
-- HTTP responses for a value no pixel renders.
-- ============================================================================

-- name: ListTagsForTenant :many
-- The plaque list: uid, status, bound location, last_ctr. The card's fifth column,
-- LAST SEEN, is a SEPARATE query (ListTagLastSeen, below) -- the reason is a typing
-- one and it is argued there, not a scoping one.
--
-- ORDER IS THE SCREEN'S ORDER AND IT IS DETERMINISTIC: unbound stock first
-- (location_id IS NULL sorts first under NULLS FIRST), then by uid. A manager
-- opening this tab is usually there to mount something; the plaques that need an
-- action are the ones with no wall. uid is the tiebreaker so the list never
-- depends on physical row order.
--
SELECT g.uid, g.tenant_id, g.location_id, g.last_ctr, g.status, g.retired_at,
       g.replaced_by, g.created_at
FROM tags g
WHERE g.tenant_id = @tenant_id
ORDER BY g.location_id NULLS FIRST, g.uid;

-- name: GetTagForTenant :one
-- One plaque, for the detail/edit card. Same column set as the list -- no
-- aes_key_ref (see the header).
--
-- KEYED BY uid, WHICH IS PUBLIC (it is printed on the plaque and sits in the NFC
-- URL), so the tenant predicate is doing real work here rather than decorating:
-- uid is a GLOBAL primary key, so without it a manager could read another
-- tenant's plaque row by guessing nothing at all -- just by typing a uid they
-- read off a wall in another building. RLS refuses it too; this is the belt.
SELECT g.uid, g.tenant_id, g.location_id, g.last_ctr, g.status, g.retired_at,
       g.replaced_by, g.created_at
FROM tags g
WHERE g.tenant_id = @tenant_id
  AND g.uid = @uid;

-- name: AssignTagToLocation :one
-- BIND: move a loaded plaque out of stock and onto a wall.
--
-- 🔴 THE PRECONDITION IS IN THE STATEMENT, NOT IN THE CALLER. `status =
-- 'unassigned'` in the WHERE makes this a single atomic transition: two managers
-- binding the same plaque to two different entrances produce ONE winner and one
-- pgx.ErrNoRows, with no read-then-write window in between (the section 4.4
-- shape, applied to a different column). A caller that checked the status first
-- and then updated would have exactly the TOCTOU hole that shape exists to
-- forbid.
--
-- BOTH COLUMNS MOVE IN ONE STATEMENT because 00013's CHECKs require it:
-- tags_unassigned_has_no_location forbids an unassigned row from holding a
-- location, and tags_active_requires_location forbids an active row from
-- lacking one. So "set the location" and "make it active" cannot be two
-- statements -- the schema refuses the intermediate state, which is the point.
--
-- THE LOCATION IS NOT VALIDATED HERE AND DOES NOT NEED TO BE: the composite FK
-- (location_id, tenant_id) -> locations (id, tenant_id) refuses an id from
-- another tenant with 23503. The screen turns that into a sentence; it is not a
-- 500 and it is not a silent success.
--
-- THE ::uuid CAST CARRIES WEIGHT (the M6-06 phase A lesson about actor_id/target):
-- 00013 made location_id nullable, so without the cast sqlc emits *uuid.UUID and a
-- nil would mean "bind this plaque to nowhere". The schema still refuses it
-- (tags_active_requires_location -> 23514), but a parameter that cannot express
-- the mistake is better than one that has to be caught.
UPDATE tags
SET location_id = @location_id::uuid,
    status = 'active'
WHERE tenant_id = @tenant_id
  AND uid = @uid
  AND status = 'unassigned'
RETURNING uid, tenant_id, location_id, last_ctr, status, retired_at, replaced_by, created_at;

-- name: RetireTagForReplacement :one
-- RETIRE: the old plaque of a "Replace tag", stamped with what replaced it.
--
-- THE ROW IS NOT DELETED AND CANNOT BE (00004 revokes DELETE from tappa_app):
-- historical transactions resolve through tag_uid and must keep resolving
-- (section 4.6). Retirement is a status plus a timestamp plus a pointer.
--
-- `status = 'active'` IN THE WHERE, for the same atomicity reason as the bind,
-- and one more: it makes retirement IDEMPOTENT-SAFE in the honest direction. A
-- second POST matches zero rows and returns pgx.ErrNoRows, so the caller can
-- tell "already retired" from "just retired" instead of silently re-stamping
-- retired_at -- which would rewrite the audit answer to "when did this plaque
-- leave the wall".
--
-- last_ctr IS NOT TOUCHED, and 00013's tags_counter_monotonic trigger is the
-- reason this is worth a sentence: the trigger fires only when last_ctr would go
-- BACKWARDS, so a retire that leaves it alone passes. An earlier proposal for
-- that trigger required a strict INCREASE on every update, which would have
-- refused exactly this statement -- the central write of the replace flow.
--
-- @replaced_by IS THE NEW PLAQUE'S uid and the self-FK (replaced_by, tenant_id)
-- -> tags (uid, tenant_id) forces it to be a plaque of the SAME tenant that
-- already exists. So the new row must be loaded before the old one can point at
-- it, which is the correct order for the audit chain anyway.
--
-- THE ::char(14) CAST, same reason as the bind's ::uuid: replaced_by is nullable
-- in the schema (most plaques are never replaced), so without it sqlc emits
-- *string and this query -- whose whole subject is the replacement -- would accept
-- "retire, replaced by nothing". That is a legitimate state for a LOST plaque and
-- it deserves its own statement, not a nil in this one.
--
-- 🔴 AND THAT CAST SILENTLY TRUNCATES, WHICH IS WHY THE LAST PREDICATE EXISTS.
-- Measured: 'AABBCCDDEEFF01ZZZZZZ'::char(14) -> 'AABBCCDDEEFF01', length 14, NO
-- ERROR. Postgres truncates an over-long value to a char(n) without complaint. So
-- an unvalidated successor uid does not fail -- it becomes a DIFFERENT, possibly
-- REAL plaque, and the self-FK then happily accepts it because that plaque exists.
-- The audit chain would point at the wrong successor and nothing would say so.
--
-- The guard is on the UNCAST parameter (`::text`), so it sees the value the caller
-- actually sent rather than the truncated one, and it is the SAME canonical form
-- 00013 put on the column (tags_uid_canonical_hex). An over-long, lower-case or
-- malformed uid now matches zero rows -> pgx.ErrNoRows, which the caller already
-- handles as "no such plaque". Handler-boundary validation is still owed (section
-- 7); this makes forgetting it non-catastrophic instead of silent.
UPDATE tags
SET status = 'retired',
    retired_at = now(),
    replaced_by = @replaced_by::char(14)
WHERE tenant_id = @tenant_id
  AND uid = @uid
  AND status = 'active'
  AND @replaced_by::text ~ '^[0-9A-F]{14}$'
RETURNING uid, tenant_id, location_id, last_ctr, status, retired_at, replaced_by, created_at;

-- name: ListTagLastSeen :many
-- "Last seen" for every plaque of this tenant that HAS been tapped -- the fifth
-- column of the M6-06 plaque list. The caller joins it to ListTagsForTenant by uid.
--
-- 🔴 WHY IT IS A SEPARATE QUERY AND NOT A COLUMN ON THE LIST, which is what it
-- obviously should be. Both shapes were written and measured; the column loses on
-- TYPE HONESTY, not on cost. As a correlated subquery the value is NULL for a
-- never-tapped plaque, and sqlc v1.28 cannot be persuaded to call it nullable:
--
--   (SELECT max(...) ...)              -> LastSeen interface{}   (untyped)
--   (SELECT max(...) ...)::timestamptz -> LastSeen time.Time     (NOT NULL -- a LIE)
--   LEFT JOIN LATERAL ... max(...)     -> LastSeen interface{}
--   LEFT JOIN LATERAL ... ORDER BY/LIMIT 1 -> LastSeen time.Time (still a lie)
--
-- The third and fourth compile and then fail at run time on exactly the rows this
-- milestone exists for -- measured, by this file's own control:
--   `can't scan into dest[8] (col: last_seen): cannot scan NULL into *time.Time`
-- on a stock plaque, i.e. every plaque that has never been tapped.
--
-- 🔴 AND ACCEPTING time.Time WITH A ZERO VALUE FOR "NEVER TAPPED" WOULD BE THE SAME
-- MISTAKE THIS MILESTONE IS ALREADY HANDING TO THE APPLICATION ROUND. tags.location_id
-- became nullable in 00013 and google/uuid scans SQL NULL into uuid.Nil WITHOUT AN
-- ERROR, so an unbound plaque arrives at the tap path looking like a plaque bound to
-- the zero location. A zero time.Time meaning "never" is that bug's twin: a value
-- that is silently valid, orderable and formattable, and wrong. So the absence is
-- represented by ABSENCE -- a plaque with no row here has never been tapped, and
-- nothing can mistake that for a timestamp.
--
-- The GROUP BY makes the type honest for free: a group EXISTS only when it has
-- rows, so max() over it is genuinely NOT NULL and `time.Time` is the truth.
--
-- COST -- MEASURED FOR THIS STATEMENT, 2026-08-09, and stated with the population
-- because a timing without one is how this repository has been wrong before.
-- Tenant 10000000-...-0001: 11 plaques, ~29 800 transactions of which ~24 900 carry
-- a tag_uid, out of ~248 000 rows in a 238 MB relation.
--
--   EXPLAIN (ANALYZE, BUFFERS): HashAggregate over a Bitmap Heap Scan on
--   transactions, driven by a Bitmap Index Scan on transactions_tenant_location_idx
--   -- the index 00013 DOES add (T11) -- ~5 090 shared buffers, 3 output rows
--   (only 3 of the 11 plaques have ever been tapped).
--   Execution Time: 31.7-38.1 ms over SIX runs (median ~32).
--
--   ⚠️ AN EARLIER VERSION OF THIS LINE SAID "21.2 / 24.7 / 26.1 ms" and that was the
--   WARM END OF ONE THREE-RUN SAMPLE. An independent measurement of the same
--   statement got 34.5 / 36.2 / 35.6 -- same plan, same node, same buffer count,
--   ~40% more wall clock. The shape is what reproduces; the absolute is a property
--   of the machine and the moment. This repo's own rule, learned three times over on
--   `make test` timings: write a RANGE with the number of runs, or write nothing.
--
-- It is ONE pass over the tenant's rows, not one subquery per plaque; that shape,
-- not any new index, is what makes the column shippable (the per-plaque correlated
-- form measured 195-203 ms on the same data -- see 00013's "Last seen" block).
--
-- ⚠️ THIS PARAGRAPH USED TO CLAIM "one Index Only Scan over
-- transactions_tenant_tag_occurred_idx (00013)" AND EVERY WORD OF IT WAS WRONG:
-- that index does not exist (00013 measures it and explicitly does NOT add it), the
-- node is a Bitmap Heap Scan and not an Index Only Scan, and the pointer to "the
-- numbers that justify it" led to the paragraph explaining why it was REJECTED. The
-- claim was written when an earlier draft did add the index and was not re-measured
-- when that decision reversed. It shipped in the generated mirror too
-- (internal/store/tags.sql.go), which is the reason it is corrected here rather
-- than deleted: the comment is the only place either file explains this cost.
--
-- WHAT IT DOES DEPEND ON, measured by dropping it inside a rolled-back transaction:
-- without transactions_tenant_location_idx the planner falls back to
-- transactions_tenant_occurred_idx, reads 69 575 index rows instead of 29 575, and
-- the statement takes 47.7 ms with 486 buffer reads. So T11's index helps this
-- query as the TENANT filter, which is a second reason it earns its place -- and it
-- is a constant factor, not a change of shape.
--
-- tag_uid IS NOT NULL: manual rows (channel='manual') carry no plaque, and a NULL
-- group would be a row the caller can never join to anything.
SELECT x.tag_uid, max(x.occurred_at)::timestamptz AS last_seen
FROM transactions x
WHERE x.tenant_id = @tenant_id
  AND x.tag_uid IS NOT NULL
GROUP BY x.tag_uid;

-- name: UnmountTagFromWall :one
-- UN-BIND: take a plaque off its wall and put it back in stock.
--
-- 🔴 IT EXISTS BECAUSE MOUNTING WAS OTHERWISE IRREVERSIBLE, AND TWO SHIPPED COMMENTS
-- CLAIMED THE OPPOSITE WITH A "MEASURED" LABEL ON THEM. An audit drove it: a plaque
-- mounted at the wrong entrance could not be moved, could not be returned to stock,
-- and — with no spare in the box — its card offered NOTHING at all. Meanwhile every
-- tap at that door was judged against the WRONG venue's IP and coordinate, so it
-- reached section 5 row 7 and landed in the approval queue. The schema always
-- permitted the reverse; the product simply had no statement for it.
--
-- BOTH COLUMNS MOVE IN ONE STATEMENT, for the same reason AssignTagToLocation's two
-- do: 00013's CHECKs bind them. tags_active_requires_location forbids an active row
-- without a location and tags_unassigned_has_no_location forbids an unassigned row
-- with one, so "clear the wall" and "return it to stock" cannot be two statements —
-- the schema refuses the intermediate state, which is the point.
--
-- `status = 'active'` IN THE WHERE, the section 4.4 shape applied to a third column:
-- two managers pressing at once produce ONE winner and one pgx.ErrNoRows, with no
-- read-then-write window. A second press matches nothing, so "already off the wall"
-- is distinguishable from "just taken off" rather than silently re-writing the row.
--
-- 🔴 IT IS NOT A RETIREMENT AND MUST NOT BECOME ONE. retired_at and replaced_by are
-- untouched: this plaque is going back in the box to be mounted somewhere else, not
-- leaving service. The audit answer "when did this plaque leave the wall for good"
-- belongs to RetireTagForReplacement and stays there.
--
-- last_ctr IS NOT TOUCHED, so 00013's tags_counter_monotonic trigger never fires —
-- it is armed only on a BACKWARDS move (RetireTagForReplacement carries the same
-- note, and the same earlier proposal would have refused this statement too).
UPDATE tags
SET location_id = NULL,
    status = 'unassigned'
WHERE tenant_id = @tenant_id
  AND uid = @uid
  AND status = 'active'
RETURNING uid, tenant_id, location_id, last_ctr, status, retired_at, replaced_by, created_at;

-- name: CountTenantPlaques :one
-- HOW MANY PLAQUES THIS BUSINESS HAS, AND HOW MANY OF THEM A TAP CAN ACTUALLY USE.
--
-- IT EXISTS FOR ONE SENTENCE ON ONE SCREEN (M7-03 phase B): the panel's landing
-- section could not tell "a quiet day" apart from "this business has never been
-- able to record anything", so it told a customer on their first afternoon to pick
-- another day. Answering that needs a fact about the BUSINESS, and the day query
-- next door only knows about the day.
--
-- 🔴 THREE COUNTS RATHER THAN ONE, BECAUSE "CANNOT RECORD" HAS THREE CAUSES AND
-- THEY NEED THREE DIFFERENT SENTENCES. 00013's four statuses split them: nothing
-- loaded at all, loaded but still in the box ('unassigned'), and every plaque out
-- of service ('retired' or 'lost'). A single "no plaque works" count would tell a
-- manager with three in a drawer that Tappa had sent nothing, and would tell a
-- business whose last plaque was retired that its replacement is in the box.
-- in_stock is counted rather than inferred from loaded - in_service for exactly
-- that reason: the leftover is retired and lost, which is a third thing.
--
-- 🔴 `status = 'active'` IS NOT A SYNONYM FOR "HAS A WALL" AND THAT IS WHY IT IS
-- THE PREDICATE. It is the same test the tap path applies (section 5 row 1: lost,
-- retired and unassigned all reject), so the screen's claim "no tap can be recorded
-- yet" is derived from the condition that would actually reject the tap rather than
-- from a second opinion about it. 00013's tags_active_requires_location makes the
-- wall implied by it.
--
-- 🔴 NO uid, NO location_id, NO aes_key_ref -- SEE THIS FILE'S HEADER. The caller
-- needs two integers and a query that returned rows would put a plaque identifier
-- into template data for a screen that renders none of it.
--
-- 🔴 COST, MEASURED -- AND IT IS **NOT** AN INDEX-ONLY SCAN. `status` is not in
-- tags_tenant_idx (tenant_id, location_id), so the two FILTERs force the heap. This
-- line first read "three counts over tags_tenant_idx", which implies index-only and
-- is the SAME MISTAKE this file already carries a correction for on ListTagLastSeen.
--
-- EXPLAIN (ANALYZE, BUFFERS) as tappa_app with app.tenant_id set, 5 runs, load
-- 2.6-4.1, 2026-08-14. The table held 40,544 rows across 34,545 tenants (12 MB); the
-- tenant asked for was the seeded KF, with 15 of them:
--
--   Aggregate <- Result (One-Time Filter: the RLS policy) <- Bitmap Heap Scan on tags
--     <- Bitmap Index Scan on tags_tenant_idx
--   Heap Blocks: exact=5   Buffers: shared hit=8   (3 index + 5 heap)
--   Execution Time: 0.209 / 0.217 / 0.232 / 0.358 / 1.433 ms
--
-- The scanned set is still the size of ONE customer's estate -- tens of plaques, not
-- thousands -- so the heap access is five pages rather than a table scan. What is
-- being recorded here is that it IS heap access, at that tenant size and that row
-- count, and not a claim that it is free.
SELECT count(*) FILTER (WHERE g.status = 'active')::bigint     AS in_service,
       count(*) FILTER (WHERE g.status = 'unassigned')::bigint AS in_stock,
       count(*)::bigint                                        AS loaded
FROM tags g
WHERE g.tenant_id = @tenant_id;
