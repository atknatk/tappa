-- +goose NO TRANSACTION
-- 00050 -- M11 API-2: the punch feed's history. Every single-tenant punch that enters the feed
-- (K-4: an ok tap on nfc or qr that is not practice, or an approved flag of the same) and has no
-- event yet gets one, in its tenant's feed, numbered after the tenant's counter -- in SMALL
-- TRANSACTIONS, each holding one tenant's counter row for one chunk.
--
-- NORMATIVE SOURCE: docs/adr/0028-dis-api-anahtar-akis-webhook.md §4 "Geri doldurma" -- "eşik 2 sn,
-- aşılırsa geri doldurma tenant başına küçük işlemlere bölünür" -- and §5 (the counter's proof).
--
-- 🔴 WHY NOT ONE TRANSACTION UNDER 00049's LOCK (ADR 0028 §4's first design; measured, a throwaway
-- PostgreSQL 17 container, 2026-10-14). The same three statements on scratch copies of
-- transactions and transaction_reviews -- every tap waits on that lock -- took 5.2 s at 700 000
-- rows (595 000 events) and 14.5 s at 2 000 000: past the 2 s threshold at the size the earlier
-- rounds measured as production's stand-in (688 000). So 00049 installs the triggers under a
-- short lock and this file writes the history afterwards, chunk by chunk:
--   * per chunk, ONE transaction, in two halves. FIRST, with no lock: the WINDOW -- the next
--     FEED_CHUNK punches of the tenant that enter the feed and have no event yet, by KEYSET on
--     (occurred_at, id) after the previous window's last row, through transactions' (tenant_id,
--     occurred_at) index. The cost of a window is its own rows, never the tenant's whole history
--     (the API-2 review's O-1: the first version re-read, sorted and NOT-EXISTS-probed every
--     eligible row of the tenant for every chunk -- O(N) a chunk, O(N^2) in all; measured on one
--     650 000-row tenant: median chunk 982 ms, longest 7 118 ms, 49 of 326 over 2 s, and without
--     ANALYZE 172 live 55P03 in a 62 s run). THEN, under the lock: the tenant's counter row
--     FOR UPDATE (the live triggers raise the same row, so a live event and a chunk of history
--     serialise on it and commit in seq order -- ADR 0028 §5's proof, unchanged); in a NEW
--     snapshot (READ COMMITTED: each statement takes one) the window's punches that still have no
--     event -- a live event committed meanwhile is seen and its punch skipped -- numbered after
--     the counter in (occurred_at, id) order; the counter moved to the last; COMMIT;
--   * so what a tap of that tenant waits for is one window's inserts behind the counter --
--     bounded by FEED_CHUNK -- far under the triggers' 2 s lock_timeout; other tenants' taps never
--     wait on it. That bound needs the PLAN to be the text's, and the second version's claim that
--     enable_* settings made it so "whatever the statistics say" was measured false (the API-2
--     review, round 3): with punch_events' statistics taken full and the table emptied, the
--     window's NOT EXISTS became a Nested Loop Anti Join over a Materialize of the tenant's events
--     (a window of over 7 minutes; hash join off made it worse), and with no statistics the locked
--     insert took 4.4 s a chunk and 270 live 55P03. So now (round 3) the TEXT fixes the plan: every
--     lookup is a scalar subquery or a LATERAL one fenced by OFFSET 0, on a unique id, the tenant
--     compared by IS NOT DISTINCT FROM (which no index serves -- every other index leads with
--     tenant_id, and statistics that call a tenant small price a scan of its whole range at one
--     row: measured, punch_events_tenant_time_idx chosen for the probe), punch_events_once leads
--     with punch_id (00048), and per chunk sequential and bitmap scans, a full Sort and JIT are off.
--     Measured, one 650 000-row tenant in the REAL tables of a throwaway container (553 chunks):
--     with this file's ANALYZE, with no statistics at all, and with punch_events' taken full and the
--     table emptied -- window median 3-4 ms (longest 8-59 ms), counter held median 64-76 ms
--     (longest 0.10-0.32 s), the first and the last 50 chunks alike; with 2 820 live taps of the
--     tenant during it, 0 failed, the longest wait 0.23 s, the feed 1..n, one event per punch.
--     The tables are still ANALYZEd first: it costs one pass and no plan depends on it.
-- Nothing slips between: 00049 created the triggers under SHARE ROW EXCLUSIVE on transactions and
-- transaction_reviews -- a row committed before that has no event, every row after has the
-- triggers'; this file takes exactly the rows with no event.
--
-- ⚠️ NAMED (ADR 0028's API-2 note): (a) a tenant's HISTORY may be numbered AFTER live events of
-- that tenant written between 00049 and this file's chunk (the cursor still skips nothing; the
-- seq order is not the occurred_at order across that boundary); within the history, ties of
-- occurred_at go by id; (b) a backfilled event's snapshot
-- is the employee's and the venue's ref, code, name and company code AT THIS MIGRATION, not at
-- the punch (K-32 cannot be reconstructed for the past); its recorded_at is the row's created_at
-- (an approved flag's: its approval's reviewed_at); (c) holding rows (site_tenant_id set --
-- development and CI databases only; production has no holding before 00049) are not backfilled;
-- (d) no delivery row is written for a backfilled event (ADR 0028 §4); (e) a row with no direction
-- (transactions.type NULL -- the product writes one on every ok and flag row since M4-04; a raw
-- writer's or an older row) is not a punch (K-40) and gets no event, like the triggers.
--
-- RE-RUNNABLE: it writes only what has no event, so a run cut short (goose records the version
-- only at the end of a NO TRANSACTION file) is finished by the next; and after a Down of 00049 and
-- its Up again, this file's Up again writes the punches recorded while the writers were down.
-- Every run walks every tenant's eligible rows once, with no lock: a tenant with nothing missing
-- costs that one unlocked pass (measured: 650 000 rows, all with events, 1.8 s, cold cache).
-- lock_timeout 1 s on each chunk's counter lock: a counter held by a stuck writer fails the run
-- (the migrate Job is red, nothing half-written is wrong -- every committed chunk is whole) rather
-- than queueing the tenant's taps behind it.

-- +goose Up
-- +goose StatementBegin
ANALYZE public.transactions;
-- +goose StatementEnd
-- +goose StatementBegin
ANALYZE public.transaction_reviews;
-- +goose StatementEnd
-- +goose StatementBegin
ANALYZE public.punch_events;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE
    -- FEED_CHUNK: the most punches one window holds -- what one transaction writes for one tenant.
    v_chunk  CONSTANT integer := 1000;
    v_tenant uuid;
    v_ids    uuid[];
    v_at     timestamptz;
    v_id     uuid;
    v_n      bigint;
BEGIN
    IF pg_catalog.to_regprocedure('public.punch_feed()') IS NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g WHERE g.tgname = 'transactions_punch_feed') THEN
        RAISE EXCEPTION 'migration 00050 (M11 API-2) needs migration 00049 (the feed''s writers): the history is written after them. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- Every tenant, not "the tenants with a punch missing": that question is the whole table's
    -- anti-join, planned by statistics this file cannot trust (below); a tenant with nothing
    -- missing costs one pass of its window, with no lock.
    FOR v_tenant IN SELECT s.id FROM public.tenants AS s ORDER BY s.id
    LOOP
        v_at := '-infinity';
        v_id := '00000000-0000-0000-0000-000000000000';
        LOOP
            -- THE PLAN IS THE TEXT'S, NOT THE STATISTICS' (the API-2 review, round 3). Every lookup
            -- below is a SCALAR subquery or a LATERAL one fenced by OFFSET 0 -- PostgreSQL never turns
            -- either into a join, a Materialize or a hashed SubPlan, so it runs once per row as an
            -- index probe -- and its ONLY sargable predicate is a unique id: the tenant is compared
            -- with IS NOT DISTINCT FROM, which no index can serve. That matters because every other
            -- index of these tables LEADS with tenant_id: given "tenant_id = $1 AND punch_id = $2",
            -- statistics that call the tenant small (punch_events ANALYZEd empty -- this very file's
            -- first state -- or before the tenant's rows) price an index scan of the tenant's whole
            -- (tenant_id, punch_time) range at one row, the same as the unique probe, and the planner
            -- took it (measured: punch_events_tenant_time_idx chosen, O(the tenant's events) a probe).
            -- So punch_events_once leads with punch_id (00048) and the probe names punch_id alone.
            -- The walk itself is the one path that yields (occurred_at) order for one tenant --
            -- transactions_tenant_occurred_idx, then an Incremental Sort on id -- once sequential and
            -- bitmap scans and a full Sort are off for this chunk's transaction (transaction-local).
            -- And JIT off: a path the settings refuse is still priced (disable_cost), so the plans
            -- here cost past jit_above_cost and every chunk's statements were compiled -- measured,
            -- 235 ms of a 237 ms window, 1.7 ms without.
            -- Measured (a throwaway container, one 650 000-row tenant, ADR 0028 API-2 note round 3):
            -- with no statistics, with punch_events' taken full and the table emptied, and with this
            -- file's ANALYZE, the windows and the locked writes of the first and the last 50 chunks
            -- take the same time.
            PERFORM pg_catalog.set_config('enable_seqscan', 'off', true);
            PERFORM pg_catalog.set_config('enable_bitmapscan', 'off', true);
            PERFORM pg_catalog.set_config('enable_sort', 'off', true);
            PERFORM pg_catalog.set_config('jit', 'off', true);
            -- The window, with no lock: keyset on (occurred_at, id).
            SELECT pg_catalog.array_agg(w.id ORDER BY w.occurred_at, w.id),
                   (pg_catalog.array_agg(w.occurred_at ORDER BY w.occurred_at DESC, w.id DESC))[1],
                   (pg_catalog.array_agg(w.id ORDER BY w.occurred_at DESC, w.id DESC))[1]
              INTO v_ids, v_at, v_id
              FROM (SELECT t.id, t.occurred_at
                      FROM public.transactions AS t
                     WHERE t.tenant_id = v_tenant
                       AND t.occurred_at >= v_at AND (t.occurred_at > v_at OR t.id > v_id)
                       AND t.site_tenant_id IS NULL
                       AND t.channel IN ('nfc', 'qr') AND t.practice IS FALSE AND t.type IS NOT NULL
                       AND (t.verdict = 'ok'
                            OR (t.verdict = 'flag'
                                AND (SELECT r.id FROM public.transaction_reviews AS r
                                      WHERE r.transaction_id = t.id AND r.tenant_id IS NOT DISTINCT FROM v_tenant
                                        AND r.outcome = 'approved') IS NOT NULL))
                       AND (SELECT 1 FROM public.punch_events AS p
                             WHERE p.punch_id = t.id AND p.tenant_id IS NOT DISTINCT FROM v_tenant LIMIT 1) IS NULL
                     ORDER BY t.occurred_at, t.id
                     LIMIT v_chunk) AS w;
            EXIT WHEN v_ids IS NULL;

            -- Under the tenant's counter lock: the window's punches that still have no event.
            PERFORM pg_catalog.set_config('lock_timeout', '1s', true);
            INSERT INTO public.feed_counters (tenant_id, last_seq) VALUES (v_tenant, 0)
            ON CONFLICT (tenant_id) DO NOTHING;
            PERFORM 1 FROM public.feed_counters AS c WHERE c.tenant_id = v_tenant FOR UPDATE;

            -- Driven by the window's ids, row by row, every lookup as above (a scalar subquery or a
            -- LATERAL one fenced by OFFSET 0, on a unique id): under the lock the cost is the window's,
            -- never the tenant's. (Measured with no statistics, a plain JOIN of the ids to
            -- transactions: a nested loop over the tenant's every row, 20-63 s a chunk UNDER THE LOCK,
            -- until PL/pgSQL's plan cache went generic after five.)
            WITH pending AS (
                SELECT t.id AS punch_id, t.occurred_at,
                       CASE WHEN t.verdict = 'flag'
                            THEN coalesce((SELECT r.reviewed_at FROM public.transaction_reviews AS r
                                            WHERE r.transaction_id = t.id AND r.tenant_id IS NOT DISTINCT FROM v_tenant AND r.outcome = 'approved'),
                                          t.created_at)
                            ELSE t.created_at END AS recorded_at,
                       t.channel, t.type, t.employee_id, t.location_id, t.tag_uid
                  FROM pg_catalog.unnest(v_ids) AS u(id)
                 CROSS JOIN LATERAL (SELECT x.* FROM public.transactions AS x
                                      WHERE x.id = u.id AND x.tenant_id IS NOT DISTINCT FROM v_tenant OFFSET 0) AS t
                 WHERE (t.verdict = 'ok'
                        OR (t.verdict = 'flag'
                            AND (SELECT r.id FROM public.transaction_reviews AS r
                                  WHERE r.transaction_id = t.id AND r.tenant_id IS NOT DISTINCT FROM v_tenant
                                    AND r.outcome = 'approved') IS NOT NULL))
                   AND (SELECT 1 FROM public.punch_events AS p
                         WHERE p.punch_id = t.id AND p.tenant_id IS NOT DISTINCT FROM v_tenant LIMIT 1) IS NULL
            ), numbered AS (
                SELECT pd.*,
                       (SELECT c.last_seq FROM public.feed_counters AS c WHERE c.tenant_id = v_tenant)
                       + row_number() OVER (ORDER BY pd.occurred_at, pd.punch_id) AS seq
                  FROM pending AS pd
            )
            INSERT INTO public.punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel,
                                             direction, employee_id, employee_ref, employee_code, employee_company, location_id,
                                             location_ref, location_name, location_company, tag_uid)
            SELECT v_tenant, n.seq, n.punch_id, v_tenant, '{employer,location}'::text[], n.occurred_at, n.recorded_at, n.channel,
                   n.type, n.employee_id, e.external_ref, e.employee_code,
                   (SELECT s.api_code FROM public.tenants AS s WHERE s.id = v_tenant), n.location_id, l.external_ref, l.name,
                   (SELECT s.api_code FROM public.tenants AS s WHERE s.id = v_tenant), n.tag_uid
              FROM numbered AS n
              LEFT JOIN LATERAL (SELECT e.external_ref, e.employee_code FROM public.employees AS e
                                  WHERE e.id = n.employee_id AND e.tenant_id IS NOT DISTINCT FROM v_tenant OFFSET 0) AS e ON true
              LEFT JOIN LATERAL (SELECT l.external_ref, l.name FROM public.locations AS l
                                  WHERE l.id = n.location_id AND l.tenant_id IS NOT DISTINCT FROM v_tenant OFFSET 0) AS l ON true;
            GET DIAGNOSTICS v_n = ROW_COUNT;

            UPDATE public.feed_counters AS c SET last_seq = c.last_seq + v_n
             WHERE c.tenant_id = v_tenant AND v_n > 0;
            COMMIT;
        END LOOP;
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Nothing to give back: the events are append-only (an event's seq is a promise to whoever read
-- it -- 00048's Down is where the feed itself refuses to go while it holds one).
SELECT 1;
