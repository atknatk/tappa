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
--   * per chunk, ONE transaction: the tenant's counter row locked FOR UPDATE first (the live
--     triggers raise the same row, so a live event and a chunk of history serialise on it and
--     commit in seq order -- ADR 0028 §5's proof, unchanged); then, in a NEW snapshot (READ
--     COMMITTED: each statement takes one), the tenant's punches with no event yet -- a live
--     event committed meanwhile is seen and its punch skipped -- the oldest FEED_CHUNK of them in
--     occurred_at order (ties: the server's instant, then the id) numbered after the counter; the
--     counter moved to the last; COMMIT;
--   * a tap of that tenant waits at most one chunk's time behind the counter (measured with the
--     chunk below: FeedBackfill_WithinTwoSeconds) -- far under the triggers' 2 s lock_timeout, so
--     no tap meets a 55P03 because of the backfill; other tenants' taps never wait on it.
-- Nothing slips between: 00049 created the triggers under SHARE ROW EXCLUSIVE on transactions and
-- transaction_reviews -- a row committed before that has no event, every row after has the
-- triggers'; this file takes exactly the rows with no event.
--
-- ⚠️ NAMED (ADR 0028's API-2 note): (a) a tenant's HISTORY may be numbered AFTER live events of
-- that tenant written between 00049 and this file's chunk (the cursor still skips nothing; the
-- seq order is not the occurred_at order across that boundary); (b) a backfilled event's snapshot
-- is the employee's and the venue's ref, code, name and company code AT THIS MIGRATION, not at
-- the punch (K-32 cannot be reconstructed for the past); its recorded_at is the row's created_at
-- (an approved flag's: its approval's reviewed_at); (c) holding rows (site_tenant_id set --
-- development and CI databases only; production has no holding before 00049) are not backfilled;
-- (d) no delivery row is written for a backfilled event (ADR 0028 §4).
--
-- RE-RUNNABLE: it writes only what has no event, so a run cut short (goose records the version
-- only at the end of a NO TRANSACTION file) is finished by the next; and after a Down of 00049 and
-- its Up again, this file's Up again writes the punches recorded while the writers were down.
-- lock_timeout 1 s on each chunk's counter lock: a counter held by a stuck writer fails the run
-- (the migrate Job is red, nothing half-written is wrong -- every committed chunk is whole) rather
-- than queueing the tenant's taps behind it.

-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    -- FEED_CHUNK: the most events one transaction writes for one tenant.
    v_chunk  CONSTANT integer := 2000;
    v_tenant uuid;
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
           AND t.channel IN ('nfc', 'qr') AND t.practice IS FALSE
           AND (t.verdict = 'ok'
                OR (t.verdict = 'flag' AND EXISTS (SELECT 1 FROM public.transaction_reviews AS r
                                                    WHERE r.transaction_id = t.id AND r.tenant_id = t.tenant_id
                                                      AND r.outcome = 'approved')))
           AND NOT EXISTS (SELECT 1 FROM public.punch_events AS p WHERE p.tenant_id = t.tenant_id AND p.punch_id = t.id)
         ORDER BY 1
    LOOP
        LOOP
            PERFORM pg_catalog.set_config('lock_timeout', '1s', true);
            INSERT INTO public.feed_counters (tenant_id, last_seq) VALUES (v_tenant, 0)
            ON CONFLICT (tenant_id) DO NOTHING;
            PERFORM 1 FROM public.feed_counters AS c WHERE c.tenant_id = v_tenant FOR UPDATE;

            WITH eligible AS (
                SELECT t.id AS punch_id, t.occurred_at, t.created_at AS recorded_at, t.channel, t.employee_id,
                       t.location_id, t.tag_uid
                  FROM public.transactions AS t
                 WHERE t.tenant_id = v_tenant AND t.site_tenant_id IS NULL
                   AND t.verdict = 'ok' AND t.channel IN ('nfc', 'qr') AND t.practice IS FALSE
                UNION ALL
                SELECT t.id, t.occurred_at, r.reviewed_at, t.channel, t.employee_id, t.location_id, t.tag_uid
                  FROM public.transactions AS t
                  JOIN public.transaction_reviews AS r ON r.transaction_id = t.id AND r.tenant_id = t.tenant_id
                 WHERE t.tenant_id = v_tenant AND t.site_tenant_id IS NULL
                   AND t.verdict = 'flag' AND r.outcome = 'approved' AND t.channel IN ('nfc', 'qr') AND t.practice IS FALSE
            ), chunk AS (
                SELECT e.* FROM eligible AS e
                 WHERE NOT EXISTS (SELECT 1 FROM public.punch_events AS p WHERE p.tenant_id = v_tenant AND p.punch_id = e.punch_id)
                 ORDER BY e.occurred_at, e.recorded_at, e.punch_id
                 LIMIT v_chunk
            ), numbered AS (
                SELECT ch.*, c.last_seq + row_number() OVER (ORDER BY ch.occurred_at, ch.recorded_at, ch.punch_id) AS seq
                  FROM chunk AS ch
                  JOIN public.feed_counters AS c ON c.tenant_id = v_tenant
            )
            INSERT INTO public.punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel,
                                             employee_id, employee_ref, employee_code, employee_company, location_id, location_ref,
                                             location_name, location_company, tag_uid)
            SELECT v_tenant, n.seq, n.punch_id, v_tenant, '{employer,location}'::text[], n.occurred_at, n.recorded_at, n.channel,
                   n.employee_id, e.external_ref, e.employee_code, s.api_code, n.location_id, l.external_ref, l.name, s.api_code, n.tag_uid
              FROM numbered AS n
              JOIN public.tenants AS s ON s.id = v_tenant
              LEFT JOIN public.employees AS e ON e.id = n.employee_id AND e.tenant_id = v_tenant
              LEFT JOIN public.locations AS l ON l.id = n.location_id AND l.tenant_id = v_tenant;
            GET DIAGNOSTICS v_n = ROW_COUNT;

            UPDATE public.feed_counters AS c SET last_seq = c.last_seq + v_n
             WHERE c.tenant_id = v_tenant AND v_n > 0;
            COMMIT;
            EXIT WHEN v_n < v_chunk;
        END LOOP;
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Nothing to give back: the events are append-only (an event's seq is a promise to whoever read
-- it -- 00048's Down is where the feed itself refuses to go while it holds one).
SELECT 1;
