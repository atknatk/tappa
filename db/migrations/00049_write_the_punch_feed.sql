-- 00049 -- M11 API-2: the punch feed's two writers. Each feed-worthy punch becomes
-- one row of punch_events per TARGET tenant, numbered by that tenant's counter row (00048), written
-- by a database trigger -- independent of the binary (an old binary and a rollback make events
-- too), with tap.Decide, internal/policy and the counter advance untouched.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §4 (K-4 the entry rule, K-5,
-- K-6, K-31 the two targets, K-32 the snapshot; the two triggers and their NAME ORDER; the
-- invariants: search_path, qualified names, explicit tenant filters, the GUC assert of the
-- own-tenant part, TG_RELID, no error-swallowing EXCEPTION block, no endpoint address read, no
-- network, lock_timeout as an attribute; the delivery row), §5 (the counter: `INSERT ... ON CONFLICT DO UPDATE ... RETURNING`
-- before each target row; K-31's canonical pre-lock for two targets; the lock invariant), §6;
-- ADR 0027 §2 (the contract of a holding trigger; the closed list -- holding_feed is already its
-- row), §3 (K-34: the definer's new grants are inside the matrix -- transactions.verdict/practice,
-- tenants.api_enabled_at, webhook_endpoints (id, tenant_id, active), punch_events and
-- webhook_deliveries INSERT, feed_counters S/I/U; url and signing_key_sealed never), §6 (the
-- transactions arm does not ask the membership; approvals ask 'strict_stmt'), §8 (the order:
-- membership, then -- if an event is written -- the two counter rows made to exist, locked in one
-- canonical statement and counted, then the event and delivery rows, then site_punches; the
-- employer's event exactly once whatever the membership; past the membership the counterpart's
-- snapshot NULL and the site skipped); CLAUDE.md §4.3, §4.5, §4.6.
--
-- WHAT IT DOES, IN ORDER (one transaction -- goose's):
--   0. lock_timeout 1 s (deploy/README.md's rule for a migration that locks what a tap waits on);
--      the precondition: 00048 in place, holding_feed still 00046's body (md5), none of this
--      file's objects, and neither definer already reaching what this file grants it;
--   1. transactions and transaction_reviews locked SHARE ROW EXCLUSIVE (CREATE TRIGGER's own mode,
--      taken first and explicitly): a row in flight commits before the triggers exist, every row
--      after has them -- so 00050's backfill (rows with no event yet) and the triggers split the
--      history between them with nothing in neither;
--   2. the grants: tappa_feedwriter's reads and its two INSERTs; tappa_holdingdefiner's K-34 rows;
--   3. punch_feed() -- the own-tenant writer, SECURITY DEFINER owned by tappa_feedwriter
--      (NOBYPASSRLS) -- and its two triggers, named AFTER the holding trigger's;
--   4. holding_feed() replaced: 00046's projection, now preceded by K-31's counter lock and the two
--      targets' events and deliveries;
--   5. the postcondition.
-- The BACKFILL is 00050's, in small transactions (ADR 0028 §4's own fallback: the single-
-- transaction backfill under this file's lock measured 5.2 s at 700 000 rows -- past the 2 s).
--
-- 🔴 §4.6 -- WHAT A FAILURE COSTS (ADR 0028 §4's named cost). No branch of either trigger
-- swallows an error: a savepoint rollback would release the counter lock and drop the event
-- silently. So a failure -- the counter's lock_timeout running out (55P03) above all -- fails the
-- employer's INSERT: an ok tap's record is NOT written (its plaque counter advanced). The tap path
-- writes the record ONCE MORE on that 55P03 (checkin.insert: the gate's and holding_feed's waits
-- since API-1b/1c, punch_feed's since this task); a hold through both waits is the named cost
-- (ADR 0027 §13 item 23). Outside the ok/approval paths nothing waits on a counter: a flag, a
-- reject, an ignored row, a manual record and a practice row return before any lock.
--

-- +goose Up

-- +goose StatementBegin
SET LOCAL lock_timeout = '1s';
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.feed_counters') IS NULL
       OR pg_catalog.to_regclass('public.punch_events') IS NULL
       OR pg_catalog.to_regclass('public.webhook_deliveries') IS NULL
       OR pg_catalog.to_regprocedure('public.feed_open_counter()') IS NULL THEN
        RAISE EXCEPTION 'migration 00049 (M11 API-2) needs migration 00048 (the feed tables). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The body this file replaces must be 00046's, byte for byte: a holding_feed changed by any
    -- other hand would be overwritten silently (00039's md5 precedent).
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.holding_feed()'::pg_catalog.regprocedure) <> 'afcbc54933fbb408c56c2420d837d5cc' THEN
        RAISE EXCEPTION 'migration 00049 (M11 API-2) replaces holding_feed, and its body is not 00046''s. Another change reached it first: rebase this file on it. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'punch_feed')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g
                   WHERE g.tgname IN ('transactions_punch_feed', 'transaction_reviews_punch_feed')) THEN
        RAISE EXCEPTION 'migration 00049 (M11 API-2) creates punch_feed and its two triggers, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- A column grant cannot narrow a wider privilege some other hand gave (00024 measured it).
    -- tappa_feedwriter reaches feed_counters alone (00048); the definer none of the feed tables
    -- and none of the three columns this file adds to its matrix.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class c
                WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
                  AND c.relname <> 'feed_counters'
                  AND (pg_catalog.has_any_column_privilege('tappa_feedwriter', c.oid, 'SELECT')
                       OR pg_catalog.has_any_column_privilege('tappa_feedwriter', c.oid, 'INSERT')
                       OR pg_catalog.has_any_column_privilege('tappa_feedwriter', c.oid, 'UPDATE')
                       OR pg_catalog.has_table_privilege('tappa_feedwriter', c.oid, 'DELETE')
                       OR pg_catalog.has_table_privilege('tappa_feedwriter', c.oid, 'TRUNCATE')))
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.feed_counters', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.feed_counters', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.feed_counters', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.punch_events', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.punch_events', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.webhook_deliveries', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.webhook_deliveries', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.webhook_endpoints', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'verdict', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'practice', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.tenants', 'api_enabled_at', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00049 (M11 API-2) gives tappa_feedwriter and tappa_holdingdefiner column privileges, and one of them already reaches what this file grants. A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. THE LOCK -- before the triggers (ADR 0028 §4 "Geri doldurma": nothing slips between them and
-- the history 00050 writes).
-- ---------------------------------------------------------------------------
-- SHARE ROW EXCLUSIVE stops every INSERT into the two tables (ROW EXCLUSIVE) until this commits and
-- is what CREATE TRIGGER takes anyway; this file does nothing long under it (no backfill -- 00050).
-- A tap that arrives meanwhile waits a few milliseconds and is then written by the new triggers.
LOCK TABLE public.transactions, public.transaction_reviews IN SHARE ROW EXCLUSIVE MODE;

-- ---------------------------------------------------------------------------
-- 2. THE GRANTS -- each one a path reads or writes (Feedwriter_EveryGrantIsIndividuallyNecessary,
-- HoldingDefiner_EveryGrantIsUsed revoke them one at a time).
-- ---------------------------------------------------------------------------
-- tappa_feedwriter (NOBYPASSRLS: every read below also meets the table's policy, in the row's own
-- tenant): the approved row's shape; the snapshot's employee, venue and company code; the gate
-- (K-22) and the active endpoint -- never its address or key; the two rows it writes.
GRANT SELECT (id, tenant_id, employee_id, location_id, tag_uid, type, occurred_at, channel, verdict, practice, site_tenant_id)
    ON transactions TO tappa_feedwriter;
GRANT SELECT (id, tenant_id, external_ref, employee_code) ON employees TO tappa_feedwriter;
GRANT SELECT (id, tenant_id, external_ref, name) ON locations TO tappa_feedwriter;
GRANT SELECT (id, api_code, api_enabled_at) ON tenants TO tappa_feedwriter;
GRANT SELECT (id, tenant_id, active) ON webhook_endpoints TO tappa_feedwriter;
GRANT INSERT (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction,
              employee_id, employee_ref, employee_code, employee_company, location_id, location_ref, location_name,
              location_company, tag_uid)
    ON punch_events TO tappa_feedwriter;
GRANT INSERT (tenant_id, endpoint_id, punch_event_seq) ON webhook_deliveries TO tappa_feedwriter;

-- tappa_holdingdefiner (ADR 0027 §3's matrix rows marked API-2): the approved row's verdict and
-- practice (K-4 on the approval arm -- the transactions arm reads NEW), both tenants' gate, the
-- active endpoint (id, tenant, switch -- the url and the key stay on the "never SELECT" list),
-- the counters (made to exist, locked, raised) and the two rows it writes.
GRANT SELECT (verdict, practice) ON transactions TO tappa_holdingdefiner;
GRANT SELECT (api_enabled_at) ON tenants TO tappa_holdingdefiner;
GRANT SELECT (id, tenant_id, active) ON webhook_endpoints TO tappa_holdingdefiner;
GRANT SELECT (tenant_id, last_seq) ON feed_counters TO tappa_holdingdefiner;
GRANT INSERT (tenant_id, last_seq) ON feed_counters TO tappa_holdingdefiner;
GRANT UPDATE (last_seq) ON feed_counters TO tappa_holdingdefiner;
GRANT INSERT (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction,
              employee_id, employee_ref, employee_code, employee_company, location_id, location_ref, location_name,
              location_company, tag_uid)
    ON punch_events TO tappa_holdingdefiner;
GRANT INSERT (tenant_id, endpoint_id, punch_event_seq) ON webhook_deliveries TO tappa_holdingdefiner;

-- ===========================================================================
-- 3. punch_feed -- the OWN-TENANT writer (ADR 0028 §4, trigger 2 of 2 in name order).
-- ===========================================================================
-- AFTER INSERT ON transactions (a row with no site) and ON transaction_reviews (an approval). A row
-- that names a site -- and an approval of one -- is holding_feed's and this body touches neither.
-- In order:
--   the entry rule (K-4, K-6) from what the row carries, BEFORE any lock: transactions -- ok on
--      nfc/qr, not practice, with a direction; reviews -- 'approved' of a flag on nfc/qr, not
--      practice, with a direction (K-40: the event carries the direction the employer computed,
--      transactions.type; the product writes one on every ok and flag row -- tap.Decide -- so a
--      row without one is a raw writer's or a pre-M4-04 row's, and is not a punch);
--   the context: NEW.tenant_id must be the transaction's app.tenant_id (asserted: this owner obeys
--      RLS, so a write into another feed is refused anyway -- the assert names it, and on an
--      approval it keeps a context-less read from finding "no row" and writing nothing);
--   the snapshot (K-32) and the gate (K-22: api_enabled_at, an active endpoint) -- read before the
--      counter, so the counter's lock is held for the writes alone;
--   one counter set per transaction: a later event-writing row of the same transaction that needs
--      a counter the first one did not lock is refused (SQLSTATE TF001), before any lock. The mark
--      (tappa.feed_counters) is a setting any session may write: one that writes it first steps
--      past the check, and the deadlock that reopens falls on OTHER tenants' holding taps (40P01,
--      their record not written -- measured, ADR 0028's API-2 note round 3). No product path
--      writes it; scripts/redline-check.sh R5c refuses the name outside db/migrations;
--   the counter raised -- the row locked until commit (ADR 0028 §5) -- the event, the delivery.
-- recorded_at is now(): the server's instant the punch entered the feed (an approved flag's is
-- its approval's).
-- +goose StatementBegin
CREATE FUNCTION public.punch_feed()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
DECLARE
    v_tenant    uuid;
    v_punch     uuid;
    v_employee  uuid;
    v_location  uuid;
    v_tag       character(14);
    v_occurred  timestamptz;
    v_channel   text;
    v_type      text;
    v_verdict   text;
    v_practice  boolean;
    v_site      uuid;
    v_code      text;
    v_api_on    timestamptz;
    v_ref       text;
    v_emp_code  text;
    v_loc_ref   text;
    v_loc_name  text;
    v_endpoint  uuid;
    v_seq       bigint;
    v_xact      text;
    v_held      text;
BEGIN
    IF TG_WHEN <> 'AFTER' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID NOT IN ('public.transactions'::pg_catalog.regclass, 'public.transaction_reviews'::pg_catalog.regclass) THEN
        RAISE EXCEPTION 'punch_feed: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF TG_RELID = 'public.transactions'::pg_catalog.regclass THEN
        IF NEW.site_tenant_id IS NOT NULL THEN
            RETURN NULL;
        END IF;
        IF NEW.verdict <> 'ok' OR NEW.channel NOT IN ('nfc', 'qr') OR NEW.practice IS NOT FALSE OR NEW.type IS NULL THEN
            RETURN NULL;
        END IF;
        v_tenant   := NEW.tenant_id;
        v_punch    := NEW.id;
        v_employee := NEW.employee_id;
        v_location := NEW.location_id;
        v_tag      := NEW.tag_uid;
        v_occurred := NEW.occurred_at;
        v_channel  := NEW.channel;
        v_type     := NEW.type;
    ELSE
        IF NEW.outcome <> 'approved' THEN
            RETURN NULL;
        END IF;
        v_tenant := NEW.tenant_id;
    END IF;

    IF v_tenant IS DISTINCT FROM NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid THEN
        RAISE EXCEPTION 'punch_feed: the row''s tenant is not the transaction''s tenant context'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF TG_RELID = 'public.transaction_reviews'::pg_catalog.regclass THEN
        SELECT t.id, t.employee_id, t.location_id, t.tag_uid, t.type, t.occurred_at, t.channel, t.verdict, t.practice, t.site_tenant_id
          INTO v_punch, v_employee, v_location, v_tag, v_type, v_occurred, v_channel, v_verdict, v_practice, v_site
          FROM public.transactions AS t
         WHERE t.id = NEW.transaction_id
           AND t.tenant_id = v_tenant;
        IF NOT FOUND THEN
            -- The review's key names the row in this tenant and the context is this tenant: a miss
            -- is a broken grant or policy -- said, not skipped (§4.6: no silent drop).
            RAISE EXCEPTION 'punch_feed: the approved record is not readable in its own tenant'
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
        IF v_site IS NOT NULL THEN
            RETURN NULL;
        END IF;
        IF v_verdict <> 'flag' OR v_channel NOT IN ('nfc', 'qr') OR v_practice IS NOT FALSE OR v_type IS NULL THEN
            RETURN NULL;
        END IF;
    END IF;

    SELECT s.api_code, s.api_enabled_at INTO v_code, v_api_on
      FROM public.tenants AS s
     WHERE s.id = v_tenant;
    SELECT e.external_ref, e.employee_code INTO v_ref, v_emp_code
      FROM public.employees AS e
     WHERE e.id = v_employee
       AND e.tenant_id = v_tenant;
    IF v_location IS NOT NULL THEN
        SELECT l.external_ref, l.name INTO v_loc_ref, v_loc_name
          FROM public.locations AS l
         WHERE l.id = v_location
           AND l.tenant_id = v_tenant;
    END IF;
    IF v_api_on IS NOT NULL THEN
        SELECT w.id INTO v_endpoint
          FROM public.webhook_endpoints AS w
         WHERE w.tenant_id = v_tenant
           AND w.active;
    END IF;

    -- ONE COUNTER SET PER TRANSACTION (ADR 0028 §5's lock invariant, made mechanical -- the API-2
    -- review's D-1): the counter row(s) the transaction's FIRST event locked are the only ones a
    -- later event of the same transaction may use. A transaction that holds tenant A's counter and
    -- reaches for B's is the half of a deadlock (measured: two rows in one raw transaction, 175 of
    -- 240 attempts 40P01) -- refused here, loudly, before the lock. The mark is transaction-local and
    -- undone with a rolled-back savepoint (checkin's retry).
    v_xact := pg_catalog.pg_current_xact_id()::text;
    v_held := pg_catalog.current_setting('tappa.feed_counters', true);
    IF v_held IS NOT NULL AND pg_catalog.split_part(v_held, ':', 1) = v_xact THEN
        IF pg_catalog.strpos(v_held, v_tenant::text) = 0 THEN
            RAISE EXCEPTION 'punch feed: this event needs a counter outside the set this transaction''s first event locked; one counter set per transaction (ADR 0028 §5)'
                USING ERRCODE = 'TF001';
        END IF;
    ELSE
        PERFORM pg_catalog.set_config('tappa.feed_counters', v_xact || ':' || v_tenant::text, true);
    END IF;

    INSERT INTO public.feed_counters AS c (tenant_id, last_seq) VALUES (v_tenant, 1)
    ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1
    RETURNING c.last_seq INTO v_seq;

    INSERT INTO public.punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel,
                                     direction, employee_id, employee_ref, employee_code, employee_company, location_id,
                                     location_ref, location_name, location_company, tag_uid)
    VALUES (v_tenant, v_seq, v_punch, v_tenant, '{employer,location}'::text[], v_occurred, pg_catalog.now(), v_channel,
            v_type, v_employee, v_ref, v_emp_code, v_code, v_location, v_loc_ref, v_loc_name, v_code, v_tag);
    IF v_endpoint IS NOT NULL THEN
        INSERT INTO public.webhook_deliveries (tenant_id, endpoint_id, punch_event_seq) VALUES (v_tenant, v_endpoint, v_seq);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.punch_feed() OWNER TO tappa_feedwriter;
REVOKE ALL ON FUNCTION public.punch_feed() FROM PUBLIC;

-- SECOND IN NAME ORDER after transactions_holding_feed / transaction_reviews_holding_feed (ADR 0028
-- §4: "holding_feed" < "punch_feed"; FeedCatalog pins the order). The two touch disjoint rows.
CREATE TRIGGER transactions_punch_feed
    AFTER INSERT ON transactions
    FOR EACH ROW
    WHEN (NEW.site_tenant_id IS NULL)
    EXECUTE FUNCTION public.punch_feed();
CREATE TRIGGER transaction_reviews_punch_feed
    AFTER INSERT ON transaction_reviews
    FOR EACH ROW
    EXECUTE FUNCTION public.punch_feed();

-- ===========================================================================
-- 4. holding_feed -- the one holding trigger, its event half added (ADR 0027 §8; ADR 0028 §4, K-31).
-- ===========================================================================
-- 00046's projection is kept as it was; before it, when the row enters the feed (K-4 -- the
-- transactions arm from NEW, the approval arm from the decided row):
--   1. the membership: the transactions arm does not ask (the BEFORE gate's acceptance IS the
--      peer answer -- ADR 0027 §6); an approval asks holding_peer 'strict_stmt';
--   2. the counters -- after the one-counter-set check (punch_feed's): with the site still a peer, BOTH rows made to exist (`INSERT ... ON CONFLICT DO
--      NOTHING`, the smaller tenant id first), then locked in ONE statement `... ORDER BY tenant_id
--      FOR UPDATE` and counted -- 2, or the function raises (7th round: a missing row was skipped by
--      FOR UPDATE and the deadlock came back; the "make exist" step closed it). Past the
--      membership the employer's row alone (one target, nothing to order);
--   3. the employer's event and delivery -- EXACTLY ONCE, whatever the membership; its counterpart
--      columns (the site venue's id, ref, name and company code) NULL past the membership, the
--      plaque's uid kept (the employer's own row carries it; the contract keeps tagId filled);
--   4. with the site a peer, the site's event and delivery: the same snapshot and the same direction
--      (the employer's -- K-40), roles {location};
--   5. 00046's projection (K-4-independent; a peer only).
-- The employer's snapshot of the SITE's venue can be read only here: tappa_feedwriter (NOBYPASSRLS,
-- the employer's context) cannot see the site's rows -- which is why both targets are this
-- trigger's (K-31). The site rows' tenant is NEW.site_tenant_id (or the approved row's), asserted.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_feed()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
DECLARE
    v_txid        uuid;
    v_site        uuid;
    v_employer    uuid;
    v_employee    uuid;
    v_location    uuid;
    v_tag         character(14);
    v_ctr         integer;
    v_sun         boolean;
    v_occurred    timestamptz;
    v_channel     text;
    v_type        text;
    v_outcome     text;
    v_gap_json    jsonb;
    v_gap_num     numeric;
    v_gap         integer;
    v_code        text;
    v_ref         text;
    v_emp_code    text;
    v_name        text;
    v_verdict     text;
    v_practice    boolean;
    v_event       boolean;
    v_peer        boolean;
    v_locked      integer;
    v_seq         bigint;
    v_emp_on      timestamptz;
    v_site_code   text;
    v_site_on     timestamptz;
    v_loc_id      uuid;
    v_loc_ref     text;
    v_loc_name    text;
    v_emp_hook    uuid;
    v_site_hook   uuid;
    v_xact        text;
    v_held        text;
BEGIN
    IF TG_WHEN <> 'AFTER' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID NOT IN ('public.transactions'::pg_catalog.regclass, 'public.transaction_reviews'::pg_catalog.regclass) THEN
        RAISE EXCEPTION 'holding_feed: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF TG_RELID = 'public.transactions'::pg_catalog.regclass THEN
        IF NEW.site_tenant_id IS NULL THEN
            RETURN NULL;
        END IF;
        v_outcome := CASE
            WHEN NEW.verdict = 'ignored' THEN NULL
            WHEN NEW.channel = 'manual' THEN 'employer_manual'
            WHEN NEW.verdict = 'ok' THEN 'counted'
            WHEN NEW.verdict = 'flag' THEN 'pending_review'
            WHEN NEW.verdict = 'reject' AND NEW.matched_sid IS NOT DISTINCT FROM 'sys:tag-not-active' THEN 'refused_tag_not_active'
            WHEN NEW.verdict = 'reject' THEN 'refused'
        END;
        -- K-4: an ok tap of a real channel enters the feed; the gate's acceptance is the peer answer.
        v_event := NEW.verdict = 'ok' AND NEW.channel IN ('nfc', 'qr') AND NEW.practice IS FALSE AND NEW.type IS NOT NULL;
        v_peer  := true;
        IF v_outcome IS NULL AND NOT v_event THEN
            -- 'ignored' (a repeat inside 60 s): neither the site nor a feed is told -- and nothing is raised.
            RETURN NULL;
        END IF;
        v_txid     := NEW.id;
        v_employer := NEW.tenant_id;
        v_site     := NEW.site_tenant_id;
        v_employee := NEW.employee_id;
        v_location := NEW.site_location_id;
        v_tag      := NEW.site_tag_uid;
        v_ctr      := NEW.ctr;
        v_sun      := NEW.sun_valid;
        v_occurred := NEW.occurred_at;
        v_channel  := NEW.channel;
        v_type     := NEW.type;
        -- Nested, not one AND: SQL does not promise to evaluate the type test before the cast,
        -- and a cast of a jsonb string would raise -- costing the employer's record (§4.6).
        v_gap_json := NEW.policy_context -> 'tap:ctrGap';
        IF pg_catalog.jsonb_typeof(v_gap_json) = 'number' THEN
            v_gap_num := (v_gap_json)::numeric;
            IF v_gap_num >= 0 AND v_gap_num < 16777216 AND v_gap_num = pg_catalog.trunc(v_gap_num) THEN
                v_gap := v_gap_num::integer;
            END IF;
        END IF;
    ELSE
        v_txid := NEW.transaction_id;
        SELECT t.tenant_id, t.site_tenant_id, t.employee_id, t.site_location_id, t.site_tag_uid, t.ctr,
               t.sun_valid, t.occurred_at, t.channel, t.type, t.verdict, t.practice
          INTO v_employer, v_site, v_employee, v_location, v_tag, v_ctr, v_sun, v_occurred, v_channel, v_type,
               v_verdict, v_practice
          FROM public.transactions AS t
         WHERE t.id = NEW.transaction_id
           AND t.tenant_id = NEW.tenant_id;
        IF v_site IS NULL THEN
            RETURN NULL;
        END IF;
        -- Past the membership the employer's approval stands and its event is written; the site is
        -- told nothing (ADR 0027 §13 item 7).
        v_peer := public.holding_peer(v_employer, v_site, 'strict_stmt') IS TRUE;
        v_event := NEW.outcome = 'approved' AND v_verdict = 'flag' AND v_channel IN ('nfc', 'qr') AND v_practice IS FALSE
                   AND v_type IS NOT NULL;
        v_outcome := NULL;
        IF v_peer THEN
            v_outcome := CASE NEW.outcome WHEN 'approved' THEN 'approved' WHEN 'rejected' THEN 'rejected' END;
        END IF;
        IF v_outcome IS NULL AND NOT v_event THEN
            RETURN NULL;
        END IF;
    END IF;

    IF v_site IS NULL OR v_site = v_employer THEN
        RAISE EXCEPTION 'holding_feed: a site row must name another tenant'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT s.api_code, s.api_enabled_at INTO v_code, v_emp_on FROM public.tenants AS s WHERE s.id = v_employer;
    SELECT e.external_ref, e.employee_code, e.full_name
      INTO v_ref, v_emp_code, v_name
      FROM public.employees AS e
     WHERE e.id = v_employee
       AND e.tenant_id = v_employer;

    IF v_event THEN
        -- The snapshot of the site's venue (K-32) and both gates (K-22) -- read before the counters.
        IF v_peer THEN
            SELECT s.api_code, s.api_enabled_at INTO v_site_code, v_site_on FROM public.tenants AS s WHERE s.id = v_site;
            IF v_location IS NOT NULL THEN
                SELECT l.id, l.external_ref, l.name INTO v_loc_id, v_loc_ref, v_loc_name
                  FROM public.locations AS l
                 WHERE l.id = v_location
                   AND l.tenant_id = v_site;
            END IF;
            IF v_site_on IS NOT NULL THEN
                SELECT w.id INTO v_site_hook FROM public.webhook_endpoints AS w WHERE w.tenant_id = v_site AND w.active;
            END IF;
        END IF;
        IF v_emp_on IS NOT NULL THEN
            SELECT w.id INTO v_emp_hook FROM public.webhook_endpoints AS w WHERE w.tenant_id = v_employer AND w.active;
        END IF;

        -- One counter set per transaction (punch_feed's comment): a later event of the transaction may
        -- use only the counters its first event locked.
        v_xact := pg_catalog.pg_current_xact_id()::text;
        v_held := pg_catalog.current_setting('tappa.feed_counters', true);
        IF v_held IS NOT NULL AND pg_catalog.split_part(v_held, ':', 1) = v_xact THEN
            IF pg_catalog.strpos(v_held, v_employer::text) = 0 OR (v_peer AND pg_catalog.strpos(v_held, v_site::text) = 0) THEN
                RAISE EXCEPTION 'punch feed: this event needs a counter outside the set this transaction''s first event locked; one counter set per transaction (ADR 0028 §5)'
                    USING ERRCODE = 'TF001';
            END IF;
        ELSIF v_peer THEN
            PERFORM pg_catalog.set_config('tappa.feed_counters', v_xact || ':' || v_employer::text || ',' || v_site::text, true);
        ELSE
            PERFORM pg_catalog.set_config('tappa.feed_counters', v_xact || ':' || v_employer::text, true);
        END IF;

        -- K-31: the counter rows made to exist, then locked in ONE canonical statement, then counted.
        IF v_peer THEN
            INSERT INTO public.feed_counters (tenant_id, last_seq)
            VALUES (LEAST(v_employer, v_site), 0), (GREATEST(v_employer, v_site), 0)
            ON CONFLICT (tenant_id) DO NOTHING;
            SELECT count(*) INTO v_locked
              FROM (SELECT c.tenant_id FROM public.feed_counters AS c
                     WHERE c.tenant_id IN (v_employer, v_site)
                     ORDER BY c.tenant_id
                       FOR UPDATE OF c) AS k;
            IF v_locked <> 2 THEN
                RAISE EXCEPTION 'holding_feed: % counter rows locked for two targets', v_locked
                    USING ERRCODE = 'object_not_in_prerequisite_state';
            END IF;
        ELSE
            INSERT INTO public.feed_counters (tenant_id, last_seq) VALUES (v_employer, 0)
            ON CONFLICT (tenant_id) DO NOTHING;
            SELECT count(*) INTO v_locked
              FROM (SELECT c.tenant_id FROM public.feed_counters AS c
                     WHERE c.tenant_id = v_employer
                       FOR UPDATE OF c) AS k;
            IF v_locked <> 1 THEN
                RAISE EXCEPTION 'holding_feed: % counter rows locked for the employer', v_locked
                    USING ERRCODE = 'object_not_in_prerequisite_state';
            END IF;
        END IF;

        -- The employer's event: exactly once, whatever the membership.
        INSERT INTO public.feed_counters AS c (tenant_id, last_seq) VALUES (v_employer, 1)
        ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1
        RETURNING c.last_seq INTO v_seq;
        INSERT INTO public.punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel,
                                         direction, employee_id, employee_ref, employee_code, employee_company, location_id,
                                         location_ref, location_name, location_company, tag_uid)
        VALUES (v_employer, v_seq, v_txid, v_employer, '{employer}'::text[], v_occurred, pg_catalog.now(), v_channel,
                v_type, v_employee, v_ref, v_emp_code, v_code, v_loc_id, v_loc_ref, v_loc_name, v_site_code, v_tag);
        IF v_emp_hook IS NOT NULL THEN
            INSERT INTO public.webhook_deliveries (tenant_id, endpoint_id, punch_event_seq) VALUES (v_employer, v_emp_hook, v_seq);
        END IF;

        -- The site's event: a peer only.
        IF v_peer THEN
            INSERT INTO public.feed_counters AS c (tenant_id, last_seq) VALUES (v_site, 1)
            ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1
            RETURNING c.last_seq INTO v_seq;
            INSERT INTO public.punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel,
                                             direction, employee_id, employee_ref, employee_code, employee_company, location_id,
                                             location_ref, location_name, location_company, tag_uid)
            VALUES (v_site, v_seq, v_txid, v_employer, '{location}'::text[], v_occurred, pg_catalog.now(), v_channel,
                    v_type, v_employee, v_ref, v_emp_code, v_code, v_loc_id, v_loc_ref, v_loc_name, v_site_code, v_tag);
            IF v_site_hook IS NOT NULL THEN
                INSERT INTO public.webhook_deliveries (tenant_id, endpoint_id, punch_event_seq) VALUES (v_site, v_site_hook, v_seq);
            END IF;
        END IF;
    END IF;

    IF v_outcome IS NULL THEN
        RETURN NULL;
    END IF;
    IF v_outcome LIKE 'refused%' AND v_sun IS TRUE THEN
        INSERT INTO public.site_punches (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
                                         employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr,
                                         sun_valid, bucket_hour, occurred_at, channel, type, outcome, ctr_gap)
        VALUES (v_site, v_txid, v_employer, v_code, v_employee, v_ref, v_emp_code, v_name, v_location, v_tag, v_ctr,
                v_sun, pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC'), v_occurred, v_channel, v_type, v_outcome, v_gap)
        ON CONFLICT (tenant_id, site_tag_uid, ctr, employee_id) WHERE outcome LIKE 'refused%' AND sun_valid IS TRUE
        DO NOTHING;
    ELSIF v_outcome LIKE 'refused%' THEN
        INSERT INTO public.site_punches (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
                                         employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr,
                                         sun_valid, bucket_hour, occurred_at, channel, type, outcome, ctr_gap)
        VALUES (v_site, v_txid, v_employer, v_code, v_employee, v_ref, v_emp_code, v_name, v_location, v_tag, v_ctr,
                v_sun, pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC'), v_occurred, v_channel, v_type, v_outcome, v_gap)
        ON CONFLICT (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour) WHERE sun_valid IS NOT TRUE AND outcome LIKE 'refused%'
        DO NOTHING;
    ELSE
        INSERT INTO public.site_punches (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
                                         employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr,
                                         sun_valid, bucket_hour, occurred_at, channel, type, outcome, ctr_gap)
        VALUES (v_site, v_txid, v_employer, v_code, v_employee, v_ref, v_emp_code, v_name, v_location, v_tag, v_ctr,
                v_sun, pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC'), v_occurred, v_channel, v_type, v_outcome, v_gap);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_function_privilege('tappa_app', 'public.punch_feed()', 'EXECUTE')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.webhook_endpoints', 'url', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.webhook_endpoints', 'signing_key_sealed', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_feedwriter', 'public.webhook_endpoints', 'url', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_feedwriter', 'public.webhook_endpoints', 'signing_key_sealed', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_feedwriter', 'public.transactions', 'policy_context', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_feedwriter', 'public.employees', 'email', 'SELECT')
       OR EXISTS (SELECT 1 FROM public.feed_counters AS c
                   WHERE c.last_seq < coalesce((SELECT max(p.seq) FROM public.punch_events AS p WHERE p.tenant_id = c.tenant_id), 0))
       OR (SELECT count(*) FROM public.feed_counters) <> (SELECT count(*) FROM public.tenants) THEN
        RAISE EXCEPTION 'migration 00049 (M11 API-2): tappa_app must not EXECUTE punch_feed, neither definer may read an endpoint''s address or key, the feed writer no evidence and no e-mail, and every tenant''s counter must stand at or past its last event. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- What the Down gives back: 00046's holding_feed (byte for byte -- the Up's precondition pins its
-- md5, and TestFeed00048And00049_DownGiveBackAndUpTakesItAgain reads it again), no punch_feed and no grants of
-- this file. What it keeps: every event already in punch_events (00048's table, append-only -- an
-- event's seq is a promise to whoever read it). A tap recorded while this file is down gets no
-- event; 00050's backfill, run again after the next Up of this file, writes it, numbered after the
-- counter (no loss, a later seq).
-- ⚠️ It takes ACCESS EXCLUSIVE on transactions and transaction_reviews (DROP TRIGGER): opAtVersion
-- skips this file (no operator object), and the holding tests' Down walks lock holdingDownTables first.
-- +goose StatementBegin
SET LOCAL lock_timeout = '1s';
-- +goose StatementEnd
DROP TRIGGER IF EXISTS transaction_reviews_punch_feed ON transaction_reviews;
DROP TRIGGER IF EXISTS transactions_punch_feed ON transactions;
DROP FUNCTION IF EXISTS public.punch_feed();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_feed()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
DECLARE
    v_txid       uuid;
    v_site       uuid;
    v_employer   uuid;
    v_employee   uuid;
    v_location   uuid;
    v_tag        character(14);
    v_ctr        integer;
    v_sun        boolean;
    v_occurred   timestamptz;
    v_channel    text;
    v_type       text;
    v_outcome    text;
    v_gap_json   jsonb;
    v_gap_num    numeric;
    v_gap        integer;
    v_code       text;
    v_ref        text;
    v_emp_code   text;
    v_name       text;
BEGIN
    IF TG_WHEN <> 'AFTER' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID NOT IN ('public.transactions'::pg_catalog.regclass, 'public.transaction_reviews'::pg_catalog.regclass) THEN
        RAISE EXCEPTION 'holding_feed: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF TG_RELID = 'public.transactions'::pg_catalog.regclass THEN
        IF NEW.site_tenant_id IS NULL THEN
            RETURN NULL;
        END IF;
        v_outcome := CASE
            WHEN NEW.verdict = 'ignored' THEN NULL
            WHEN NEW.channel = 'manual' THEN 'employer_manual'
            WHEN NEW.verdict = 'ok' THEN 'counted'
            WHEN NEW.verdict = 'flag' THEN 'pending_review'
            WHEN NEW.verdict = 'reject' AND NEW.matched_sid IS NOT DISTINCT FROM 'sys:tag-not-active' THEN 'refused_tag_not_active'
            WHEN NEW.verdict = 'reject' THEN 'refused'
        END;
        IF v_outcome IS NULL THEN
            -- 'ignored' (a repeat inside 60 s): the site is not told -- and nothing is raised.
            RETURN NULL;
        END IF;
        v_txid     := NEW.id;
        v_employer := NEW.tenant_id;
        v_site     := NEW.site_tenant_id;
        v_employee := NEW.employee_id;
        v_location := NEW.site_location_id;
        v_tag      := NEW.site_tag_uid;
        v_ctr      := NEW.ctr;
        v_sun      := NEW.sun_valid;
        v_occurred := NEW.occurred_at;
        v_channel  := NEW.channel;
        v_type     := NEW.type;
        -- Nested, not one AND: SQL does not promise to evaluate the type test before the cast,
        -- and a cast of a jsonb string would raise -- costing the employer's record (§4.6).
        v_gap_json := NEW.policy_context -> 'tap:ctrGap';
        IF pg_catalog.jsonb_typeof(v_gap_json) = 'number' THEN
            v_gap_num := (v_gap_json)::numeric;
            IF v_gap_num >= 0 AND v_gap_num < 16777216 AND v_gap_num = pg_catalog.trunc(v_gap_num) THEN
                v_gap := v_gap_num::integer;
            END IF;
        END IF;
    ELSE
        v_txid := NEW.transaction_id;
        SELECT t.tenant_id, t.site_tenant_id, t.employee_id, t.site_location_id, t.site_tag_uid, t.ctr,
               t.sun_valid, t.occurred_at, t.channel, t.type
          INTO v_employer, v_site, v_employee, v_location, v_tag, v_ctr, v_sun, v_occurred, v_channel, v_type
          FROM public.transactions AS t
         WHERE t.id = NEW.transaction_id
           AND t.tenant_id = NEW.tenant_id;
        IF v_site IS NULL THEN
            RETURN NULL;
        END IF;
        IF public.holding_peer(v_employer, v_site, 'strict_stmt') IS NOT TRUE THEN
            -- The membership ended: the employer's approval stands, the site is not told (§13 item 7).
            RETURN NULL;
        END IF;
        v_outcome := CASE NEW.outcome WHEN 'approved' THEN 'approved' WHEN 'rejected' THEN 'rejected' END;
        IF v_outcome IS NULL THEN
            RETURN NULL;
        END IF;
    END IF;

    IF v_site IS NULL OR v_site = v_employer THEN
        RAISE EXCEPTION 'holding_feed: a site row must name another tenant'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT s.api_code INTO v_code FROM public.tenants AS s WHERE s.id = v_employer;
    SELECT e.external_ref, e.employee_code, e.full_name
      INTO v_ref, v_emp_code, v_name
      FROM public.employees AS e
     WHERE e.id = v_employee
       AND e.tenant_id = v_employer;

    IF v_outcome LIKE 'refused%' AND v_sun IS TRUE THEN
        INSERT INTO public.site_punches (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
                                         employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr,
                                         sun_valid, bucket_hour, occurred_at, channel, type, outcome, ctr_gap)
        VALUES (v_site, v_txid, v_employer, v_code, v_employee, v_ref, v_emp_code, v_name, v_location, v_tag, v_ctr,
                v_sun, pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC'), v_occurred, v_channel, v_type, v_outcome, v_gap)
        ON CONFLICT (tenant_id, site_tag_uid, ctr, employee_id) WHERE outcome LIKE 'refused%' AND sun_valid IS TRUE
        DO NOTHING;
    ELSIF v_outcome LIKE 'refused%' THEN
        INSERT INTO public.site_punches (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
                                         employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr,
                                         sun_valid, bucket_hour, occurred_at, channel, type, outcome, ctr_gap)
        VALUES (v_site, v_txid, v_employer, v_code, v_employee, v_ref, v_emp_code, v_name, v_location, v_tag, v_ctr,
                v_sun, pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC'), v_occurred, v_channel, v_type, v_outcome, v_gap)
        ON CONFLICT (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour) WHERE sun_valid IS NOT TRUE AND outcome LIKE 'refused%'
        DO NOTHING;
    ELSE
        INSERT INTO public.site_punches (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id,
                                         employee_ref, employee_code, employee_name, site_location_id, site_tag_uid, ctr,
                                         sun_valid, bucket_hour, occurred_at, channel, type, outcome, ctr_gap)
        VALUES (v_site, v_txid, v_employer, v_code, v_employee, v_ref, v_emp_code, v_name, v_location, v_tag, v_ctr,
                v_sun, pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC'), v_occurred, v_channel, v_type, v_outcome, v_gap);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

REVOKE INSERT (tenant_id, endpoint_id, punch_event_seq) ON webhook_deliveries FROM tappa_holdingdefiner;
REVOKE INSERT (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction,
               employee_id, employee_ref, employee_code, employee_company, location_id, location_ref, location_name,
               location_company, tag_uid)
    ON punch_events FROM tappa_holdingdefiner;
REVOKE UPDATE (last_seq) ON feed_counters FROM tappa_holdingdefiner;
REVOKE INSERT (tenant_id, last_seq) ON feed_counters FROM tappa_holdingdefiner;
REVOKE SELECT (tenant_id, last_seq) ON feed_counters FROM tappa_holdingdefiner;
REVOKE SELECT (id, tenant_id, active) ON webhook_endpoints FROM tappa_holdingdefiner;
REVOKE SELECT (api_enabled_at) ON tenants FROM tappa_holdingdefiner;
REVOKE SELECT (verdict, practice) ON transactions FROM tappa_holdingdefiner;

REVOKE INSERT (tenant_id, endpoint_id, punch_event_seq) ON webhook_deliveries FROM tappa_feedwriter;
REVOKE INSERT (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction,
               employee_id, employee_ref, employee_code, employee_company, location_id, location_ref, location_name,
               location_company, tag_uid)
    ON punch_events FROM tappa_feedwriter;
REVOKE SELECT (id, tenant_id, active) ON webhook_endpoints FROM tappa_feedwriter;
REVOKE SELECT (id, api_code, api_enabled_at) ON tenants FROM tappa_feedwriter;
REVOKE SELECT (id, tenant_id, external_ref, name) ON locations FROM tappa_feedwriter;
REVOKE SELECT (id, tenant_id, external_ref, employee_code) ON employees FROM tappa_feedwriter;
REVOKE SELECT (id, tenant_id, employee_id, location_id, tag_uid, type, occurred_at, channel, verdict, practice, site_tenant_id)
    ON transactions FROM tappa_feedwriter;
