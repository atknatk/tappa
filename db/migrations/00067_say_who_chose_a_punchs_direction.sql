-- 00067 -- M15-13 (ADR 0032 §B.6, §B.4, §A.5, "Geri alma"; ADR 0028 §4; ADR 0027 §9 item 3): the
-- punch feed says who chose a punch's direction.
--
-- WHAT. punch_events.direction_source -- 'auto' when the direction the feed carries is the one the
-- engine computed, 'employee' when the person asked for the other one and that was recorded (ADR
-- 0032 §B.3) -- a snapshot column like the rest of K-32: written once, by the trigger that writes the
-- event, never changed (punch_events is append-only, 00048). The v1 wire calls it directionSource
-- (internal/punchwire; OpenAPI Punch and PunchDelivery). Both feed writers fill it:
--   punch_feed    (tappa_feedwriter)     the own-tenant row {employer,location};
--   holding_feed  (tappa_holdingdefiner) BOTH rows of a holding punch -- the employer's {employer}
--                 and the site's {location} carry the SAME value, as they carry the same direction
--                 (K-40). The site sees the column: ADR 0027 §9 item 3's K note (2026-10-22, M15-13).
-- site_punches (00046, the site's projection) is not a punch_events row and is not changed.
--
-- THE READER RULE, AGAIN IN SQL (ADR 0032 §B.4; tap.RecordedDirectionSource is the Go copy). The
-- value comes from the punch's transactions row -- NEW on an ok tap, the approved row on an approval
-- -- through ONE expression, the same text in both bodies:
--
--   v_dsrc := CASE WHEN v_channel IN ('nfc', 'qr') AND v_type IS NOT NULL
--                  THEN CASE WHEN v_stored IS NULL THEN 'auto'
--                            WHEN v_stored IN ('auto', 'employee') THEN v_stored END END;
--
--   a directed nfc/qr row, direction_source NULL   'auto' -- before M15-11 the engine chose
--   a directed nfc/qr row, 'auto' / 'employee'     that value
--   a manual row                                   NULL -- whatever the column holds: no CHECK keeps
--                                                  a source off a manual row (tappa_app could write
--                                                  manual + 'employee'; no code path does), so the
--                                                  value there is not believed
--   an undirected row (type NULL)                  NULL -- no direction, nobody chose one
--   a value the CHECK refuses                      NULL -- not guessed at
-- The entry rule (K-4, K-6) already keeps manual and undirected rows out of the feed; the rule is
-- written in full anyway, so the feed never trusts a column the reader rule does not.
-- internal/db's punchdirectionsource_test.go reads the expression out of the INSTALLED bodies,
-- holds the two to one text, and evaluates it against tap.RecordedDirectionSource for every
-- combination. It is inline, not a function of its own: a callable both definers EXECUTE would widen
-- their reach (holdingcatalog_test.go's reach check flags tappa_holdingdefiner's EXECUTE on another
-- role's function PUBLIC may not call), for one CASE.
--
-- NULL ON AN EVENT IS A STATE: every event written before this file carries NULL and is not
-- back-filled (punch_events is append-only; §4.3's reasoning). The wire answers it as null.
--
-- PRIVILEGES -- each one a path reads or writes (measured on a throwaway PostgreSQL 17 database,
-- 2026-10-22):
--   tappa_feedwriter      SELECT (direction_source) ON transactions -- punch_feed's APPROVAL arm reads
--                         the approved row by its key (an ok tap's arm reads NEW, which needs no
--                         privilege): without it the approval of a flag is 42501, the review lost;
--                         INSERT (direction_source) ON punch_events -- the event's column.
--   tappa_holdingdefiner  the same two, for holding_feed's approval arm and its two events.
--   tappa_app             SELECT (direction_source) ON punch_events -- the API's read. Nothing else:
--                         the triggers stay the one writer (00048's matrix).
-- No other column, no table-level grant. TestFeedwriter_EveryGrantIsIndividuallyNecessary and
-- TestHoldingDefiner_EveryGrantIsUsed revoke each one alone and see a path refused.
--
-- THE CHECK IS THE SECOND SAFETY, NOT VALID (ADR 0032 §A.5's arrangement): the expression yields
-- only 'auto', 'employee' or NULL. The ALTER takes ACCESS EXCLUSIVE on punch_events, which every ok
-- tap writes; a validating ADD CONSTRAINT would scan the whole feed under it. Every row older than
-- this file is NULL and passes, so NOT VALID is chosen for the lock, not for them; it binds every
-- new row.
--
-- LOCKS. lock_timeout 1 s is the first statement of both sections (ADR 0032 §A.5, 00065/00066's
-- pins). The Up locks punch_events alone (the ALTER); CREATE OR REPLACE FUNCTION takes no table
-- lock, and a column GRANT on transactions takes none either (measured: pg_locks shows no relation
-- lock for the GRANT's transaction). A tap holds transactions and then waits for punch_events; this
-- file holds punch_events and waits for nothing a tap holds -- no cycle. Behind a long reader the
-- file gives up with 55P03 and changes nothing.
--
-- NUMBERING (2026-10-22, M15-13). The plan gave this card 00068 and M15-15 00067. M15-13 reaches
-- main first; goose refuses a migration numbered below the highest one applied ("found 1 missing
-- migrations before current version 68", goose v3.24.1, measured) and this repository runs plain
-- `up` (Makefile, deploy/k8s/30-migrate-job.yaml -- no allow-missing). So this file is 00067 and
-- punch_claims takes 00068.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                    WHERE a.attrelid = 'public.transactions'::pg_catalog.regclass
                      AND a.attname = 'direction_source' AND NOT a.attisdropped)
       OR pg_catalog.to_regclass('public.punch_events') IS NULL THEN
        RAISE EXCEPTION 'migration 00067 (M15-13) needs migration 00065 (transactions.direction_source) and 00048 (punch_events). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                WHERE a.attrelid = 'public.punch_events'::pg_catalog.regclass
                  AND a.attname = 'direction_source' AND NOT a.attisdropped) THEN
        RAISE EXCEPTION 'migration 00067 (M15-13) adds punch_events.direction_source, and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The two bodies this file replaces must be 00049's, byte for byte (00049's own md5 precedent):
    -- a trigger changed by any other hand would be overwritten silently.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.punch_feed()'::pg_catalog.regprocedure) IS DISTINCT FROM 'f9e0322dbb657a1573223168acf860d6'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.holding_feed()'::pg_catalog.regprocedure) IS DISTINCT FROM 'eb918ce9f057bf264cbfce39f8daa178' THEN
        RAISE EXCEPTION 'migration 00067 (M15-13) replaces punch_feed and holding_feed, and one of their bodies is not 00049''s. Another change reached it first: rebase this file on it. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- A column grant cannot narrow a wider privilege some other hand gave (00024 measured it): the
    -- three roles must not reach the column on transactions yet, nor hold a table-level verb on
    -- punch_events that the new column would fall under.
    IF pg_catalog.has_column_privilege('tappa_feedwriter', 'public.transactions', 'direction_source', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'direction_source', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_feedwriter', 'public.punch_events', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_feedwriter', 'public.punch_events', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_feedwriter', 'public.punch_events', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.punch_events', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.punch_events', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.punch_events', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.punch_events', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.punch_events', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.punch_events', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00067 (M15-13) gives tappa_feedwriter, tappa_holdingdefiner and tappa_app column privileges, and one of them already reaches what this file grants (a table-level verb on punch_events, or transactions.direction_source). A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The column (nullable, no default, no back-fill) and its NOT VALID CHECK.
-- ---------------------------------------------------------------------------
ALTER TABLE punch_events
    ADD COLUMN direction_source text,
    ADD CONSTRAINT punch_events_direction_source_is_known
        CHECK (direction_source IN ('auto', 'employee')) NOT VALID;

-- ---------------------------------------------------------------------------
-- 2. The grants (the header's list).
-- ---------------------------------------------------------------------------
GRANT SELECT (direction_source) ON transactions TO tappa_feedwriter;
GRANT SELECT (direction_source) ON transactions TO tappa_holdingdefiner;
GRANT INSERT (direction_source) ON punch_events TO tappa_feedwriter;
GRANT INSERT (direction_source) ON punch_events TO tappa_holdingdefiner;
GRANT SELECT (direction_source) ON punch_events TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. punch_feed -- 00049's body, plus the source: read from NEW (an ok tap) or the approved row (an
-- approval), the expression, the event's column.
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.punch_feed()
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
    v_stored    text;
    v_dsrc      text;
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
        v_stored   := NEW.direction_source;
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
        SELECT t.id, t.employee_id, t.location_id, t.tag_uid, t.type, t.occurred_at, t.channel, t.verdict, t.practice, t.site_tenant_id,
               t.direction_source
          INTO v_punch, v_employee, v_location, v_tag, v_type, v_occurred, v_channel, v_verdict, v_practice, v_site,
               v_stored
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

    -- Who chose the direction (M15-13): ADR 0032 §B.4's reader rule, 00067's header.
    v_dsrc := CASE WHEN v_channel IN ('nfc', 'qr') AND v_type IS NOT NULL THEN CASE WHEN v_stored IS NULL THEN 'auto' WHEN v_stored IN ('auto', 'employee') THEN v_stored END END;

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
                                     location_ref, location_name, location_company, tag_uid, direction_source)
    VALUES (v_tenant, v_seq, v_punch, v_tenant, '{employer,location}'::text[], v_occurred, pg_catalog.now(), v_channel,
            v_type, v_employee, v_ref, v_emp_code, v_code, v_location, v_loc_ref, v_loc_name, v_code, v_tag, v_dsrc);
    IF v_endpoint IS NOT NULL THEN
        INSERT INTO public.webhook_deliveries (tenant_id, endpoint_id, punch_event_seq) VALUES (v_tenant, v_endpoint, v_seq);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 4. holding_feed -- 00049's body, plus the source, written into the employer's row and the site's.
-- ---------------------------------------------------------------------------
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
    v_stored      text;
    v_dsrc        text;
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
        v_stored   := NEW.direction_source;
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
               t.sun_valid, t.occurred_at, t.channel, t.type, t.verdict, t.practice, t.direction_source
          INTO v_employer, v_site, v_employee, v_location, v_tag, v_ctr, v_sun, v_occurred, v_channel, v_type,
               v_verdict, v_practice, v_stored
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
        -- Who chose the direction (M15-13): ADR 0032 §B.4's reader rule, 00067's header -- one value,
        -- written into both targets' rows.
        v_dsrc := CASE WHEN v_channel IN ('nfc', 'qr') AND v_type IS NOT NULL THEN CASE WHEN v_stored IS NULL THEN 'auto' WHEN v_stored IN ('auto', 'employee') THEN v_stored END END;

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
                                         location_ref, location_name, location_company, tag_uid, direction_source)
        VALUES (v_employer, v_seq, v_txid, v_employer, '{employer}'::text[], v_occurred, pg_catalog.now(), v_channel,
                v_type, v_employee, v_ref, v_emp_code, v_code, v_loc_id, v_loc_ref, v_loc_name, v_site_code, v_tag, v_dsrc);
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
                                             location_ref, location_name, location_company, tag_uid, direction_source)
            VALUES (v_site, v_seq, v_txid, v_employer, '{location}'::text[], v_occurred, pg_catalog.now(), v_channel,
                    v_type, v_employee, v_ref, v_emp_code, v_code, v_loc_id, v_loc_ref, v_loc_name, v_site_code, v_tag, v_dsrc);
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
-- 5. POSTCONDITION -- measured as the roles meet it (has_* count membership and PUBLIC).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_column_privilege('tappa_feedwriter', 'public.transactions', 'direction_source', 'SELECT')
       OR NOT pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'direction_source', 'SELECT')
       OR NOT pg_catalog.has_column_privilege('tappa_feedwriter', 'public.punch_events', 'direction_source', 'INSERT')
       OR NOT pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.punch_events', 'direction_source', 'INSERT')
       OR NOT pg_catalog.has_column_privilege('tappa_app', 'public.punch_events', 'direction_source', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_feedwriter', 'public.transactions', 'direction_source', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'direction_source', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_feedwriter', 'public.punch_events', 'direction_source', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.punch_events', 'direction_source', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.punch_events', 'direction_source', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.punch_events', 'direction_source', 'UPDATE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.punch_feed()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_feed()', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00067 (M15-13): the feed writers must read transactions.direction_source and insert punch_events.direction_source, tappa_app read it and write nothing, and nobody but the owners execute the two triggers -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE WHO CHOSE A PUNCH'S DIRECTION REFUSES (CLAUDE.md §4.3, §4.6; ADR 0032
-- "Geri alma"; the 00065 precedent). punch_events is append-only and an event an integrator may
-- already have read is a promise: while an event carries a direction_source this Down stops and
-- changes nothing. The way back after the first such event is the binary, not the schema: the old
-- code names its punch_events columns and never this one, and the CHECK admits NULL.
--
-- punch_events is LOCKED BEFORE IT IS LOOKED AT: between an unlocked EXISTS and the ALTER a tap could
-- commit an event with a source, and the ALTER would then drop it. ACCESS EXCLUSIVE is what the
-- ALTER takes anyway; taking it first only moves it ahead of the check. Nothing else here takes a
-- table lock (the two bodies and the column REVOKEs -- the header's measurement).
SET LOCAL lock_timeout = '1s';

LOCK TABLE punch_events IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.punch_events WHERE direction_source IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00067 Down: punch_events holds events with a direction_source (who chose a punch''s direction, which integrators may have read); events are append-only. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- 00049's two bodies, byte for byte (the Up's precondition pins their md5; the Down test reads them
-- again).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.punch_feed()
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

REVOKE SELECT (direction_source) ON punch_events FROM tappa_app;
REVOKE INSERT (direction_source) ON punch_events FROM tappa_holdingdefiner;
REVOKE INSERT (direction_source) ON punch_events FROM tappa_feedwriter;
REVOKE SELECT (direction_source) ON transactions FROM tappa_holdingdefiner;
REVOKE SELECT (direction_source) ON transactions FROM tappa_feedwriter;

ALTER TABLE punch_events
    DROP CONSTRAINT punch_events_direction_source_is_known,
    DROP COLUMN direction_source;
