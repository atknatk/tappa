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
--     bounded by FEED_CHUNK, independent of the tenant's size (measured: TestFeedBackfill_* and
--     the API-2 note) -- far under the triggers' 2 s lock_timeout; other tenants' taps never wait
--     on it. The three tables are ANALYZEd first, and each chunk's transaction turns sequential
--     scans, hash and merge joins off (SET LOCAL): the window and the insert probe
--     transactions, transaction_reviews and punch_events by their indexes, whatever the
--     statistics say -- punch_events' go stale under the loop itself.
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

    FOR v_tenant IN
        SELECT DISTINCT t.tenant_id
          FROM public.transactions AS t
         WHERE t.site_tenant_id IS NULL
           AND t.channel IN ('nfc', 'qr') AND t.practice IS FALSE AND t.type IS NOT NULL
           AND (t.verdict = 'ok'
                OR (t.verdict = 'flag' AND EXISTS (SELECT 1 FROM public.transaction_reviews AS r
                                                    WHERE r.transaction_id = t.id AND r.tenant_id = t.tenant_id
                                                      AND r.outcome = 'approved')))
           AND NOT EXISTS (SELECT 1 FROM public.punch_events AS p WHERE p.tenant_id = t.tenant_id AND p.punch_id = t.id)
         ORDER BY 1
    LOOP
        v_at := '-infinity';
        v_id := '00000000-0000-0000-0000-000000000000';
        LOOP
            -- The plan, pinned to index probes for this chunk's transaction (transaction-local):
            -- punch_events grows under this very loop, so its statistics go stale from the first
            -- chunk on, and a plan chosen for an "empty" punch_events probed it by sequential
            -- scan -- O(tenant) a chunk again (measured: windows of 1.5 s on a 50 000-row tenant).
            PERFORM pg_catalog.set_config('enable_seqscan', 'off', true);
            PERFORM pg_catalog.set_config('enable_hashjoin', 'off', true);
            PERFORM pg_catalog.set_config('enable_mergejoin', 'off', true);
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
                            OR (t.verdict = 'flag' AND EXISTS (SELECT 1 FROM public.transaction_reviews AS r
                                                                WHERE r.transaction_id = t.id AND r.tenant_id = v_tenant
                                                                  AND r.outcome = 'approved')))
                       AND NOT EXISTS (SELECT 1 FROM public.punch_events AS p WHERE p.tenant_id = v_tenant AND p.punch_id = t.id)
                     ORDER BY t.occurred_at, t.id
                     LIMIT v_chunk) AS w;
            EXIT WHEN v_ids IS NULL;

            -- Under the tenant's counter lock: the window's punches that still have no event.
            PERFORM pg_catalog.set_config('lock_timeout', '1s', true);
            INSERT INTO public.feed_counters (tenant_id, last_seq) VALUES (v_tenant, 0)
            ON CONFLICT (tenant_id) DO NOTHING;
            PERFORM 1 FROM public.feed_counters AS c WHERE c.tenant_id = v_tenant FOR UPDATE;

            WITH pending AS (
                SELECT t.id AS punch_id, t.occurred_at, coalesce(r.reviewed_at, t.created_at) AS recorded_at, t.channel,
                       t.type, t.employee_id, t.location_id, t.tag_uid
                  FROM public.transactions AS t
                  LEFT JOIN public.transaction_reviews AS r
                         ON r.transaction_id = t.id AND r.tenant_id = v_tenant AND r.outcome = 'approved' AND t.verdict = 'flag'
                 WHERE t.tenant_id = v_tenant AND t.id = ANY (v_ids)
                   AND (t.verdict = 'ok' OR r.transaction_id IS NOT NULL)
                   AND NOT EXISTS (SELECT 1 FROM public.punch_events AS p WHERE p.tenant_id = v_tenant AND p.punch_id = t.id)
            ), numbered AS (
                SELECT pd.*, c.last_seq + row_number() OVER (ORDER BY pd.occurred_at, pd.punch_id) AS seq
                  FROM pending AS pd
                  JOIN public.feed_counters AS c ON c.tenant_id = v_tenant
            )
            INSERT INTO public.punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel,
                                             direction, employee_id, employee_ref, employee_code, employee_company, location_id,
                                             location_ref, location_name, location_company, tag_uid)
            SELECT v_tenant, n.seq, n.punch_id, v_tenant, '{employer,location}'::text[], n.occurred_at, n.recorded_at, n.channel,
                   n.type, n.employee_id, e.external_ref, e.employee_code, s.api_code, n.location_id, l.external_ref, l.name,
                   s.api_code, n.tag_uid
              FROM numbered AS n
              JOIN public.tenants AS s ON s.id = v_tenant
              LEFT JOIN public.employees AS e ON e.id = n.employee_id AND e.tenant_id = v_tenant
              LEFT JOIN public.locations AS l ON l.id = n.location_id AND l.tenant_id = v_tenant;
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
