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
