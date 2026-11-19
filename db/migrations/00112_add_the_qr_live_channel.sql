-- 00112 -- M16-19 (ADR 0045 §2.5, Bedel 6; Q38 (a), Q43 (b)): the channel value 'qr_live' -- a tap on a
-- live QR screen (00111) -- in every closed channel set the schema holds, and in the punch feed's
-- triggers.
--
-- NORMATIVE SOURCES: docs/adr/0045-istege-bagli-native-uygulama.md §2.5 and Bedel 6 (the vocabulary is
-- a wide surface; the list is a start, M16-21's parity matrix is the proof); docs/plan/m16-mobil.md
-- M16-19 (this contract and the inventory under it); docs/plan/open-questions.md Q38 (a) (a live QR tap
-- enters the /v1 feed -- outside as `channel: qr` with `qrKind: live`, a mapping the API makes,
-- M16-21), Q43 (b) (a holding live QR tap is refused and recorded with its site columns, and the site
-- sees the refusal); CLAUDE.md §4.3, §4.6.
--
-- WHY ONE FILE AND ALL OF IT. Forgetting one closed set either refuses the record (a CHECK: 23514,
-- §4.6) or drops it from the feed without a word (a trigger's `NOT IN`). So every set the INSTALLED
-- schema holds was measured (pg_constraint and pg_proc on a database at 00111, 2026-11-19) and each one
-- that names 'nfc' and 'qr' gains 'qr_live' here:
--   transactions.transactions_channel_check        00005:114            the vocabulary
--   transactions.transactions_site_by_channel      00043:139-142        + a THIRD arm (below)
--   transactions.transactions_shift_snapshot_shape 00095:66, :68        a directed live QR tap is
--                                                                         judged against a shift
--   punch_events.punch_events_channel              00048:171            Q38 (a): the feed stores the
--                                                                         REAL channel
--   site_punches.site_punches_channel_check        00046:137            Q43 (b): the site sees the
--                                                                         refusal
--   punch_feed()        00067:193, :234, :240     the ok-tap gate, the approval gate, the source rule
--   holding_feed()      00067:366, :408, :434     the same three, the holding twin
--   holding_site_gate() 00043:374                 the tolerant arm: a refusal on a tap channel
-- What does NOT change, and why, is the inventory under the M16-19 card (one line per place).
--
-- STORED AS 'qr_live', NOT AS 'qr' + A KIND (Q38 (a), the user's preference, measured): punch_events
-- has a channel column with a closed CHECK, so the real channel is stored and widened here; the v1
-- wire's `channel: qr` + `qrKind: live` is a mapping in internal/punchwire (M16-21 -- this file does
-- not touch the OpenAPI document or the handler). Until M16-21 nothing writes a qr_live row: no code
-- path derives the channel (tap.ChannelQRLive is named in Go for feedEligible's parity alone).
--
-- THE SITE ARM (Q43 (b)). A holding live QR tap is refused and recorded at the employer with the site
-- named: site_tenant_id and site_location_id set (the screen's tenant and location), site_tag_uid NULL
-- -- a screen is no plaque. transactions_site_by_channel gains that arm, the manual arm's shape on the
-- qr_live channel; holding_feed writes the site's `refused` row as it does for a plaque. The site's
-- refusal dedup (site_punches_other_refusal_hourly, 00046:171-173, NULLS NOT DISTINCT) keys
-- site_tag_uid, which is NULL here, so one person's live QR refusals in one hour at ANY of the site's
-- screens are ONE site row. KEPT, an accepted difference (the card's either/or, decided in the card's
-- inventory): the employer's transactions keep every attempt (§4.6 holds), the site sees that the
-- person was refused in that hour (K-1h: the outcome, not the reason), and the plaque's analogue would
-- be the SCREEN, which site_punches has no column for -- adding site_location_id to the key would
-- change tappa_holdingdefiner's matrix (ADR 0027 §3) for a projection of a tap the user closed.
--
-- THE SOURCE RULE (00067's v_dsrc). The feed's copy of ADR 0032 §B.4's reader rule names the tap
-- channels; it gains qr_live so a live QR event carries its direction's source like a static QR one,
-- and tap.RecordedDirectionSource (its Go copy, bound by
-- TestPunchDirectionSource_TheFeedsExpressionIsTheReaderRule) changes in the same commit.
--
-- NOT VALID, VALIDATED IN 00113 (00043/00044's split). Each ADD CONSTRAINT here is NOT VALID: the
-- ALTERs take ACCESS EXCLUSIVE on transactions, punch_events and site_punches, which every tap writes,
-- and a validating ADD would scan each table under it. Every existing row passes (no row holds
-- qr_live; each set only widens), and a NOT VALID CHECK binds every new row. 00113 validates the four
-- that were valid before under SHARE UPDATE EXCLUSIVE; transactions_shift_snapshot_shape stays NOT
-- VALID, as 00095 left it (its header says why).
--
-- LOCKS. lock_timeout 1 s is the first statement of both sections (the hot-table rule, 00064-00066).
-- The three tables are locked in ONE statement, in the order a tap takes them (transactions, then
-- the feed and the projection its triggers write) -- a tap that holds transactions and waits for
-- punch_events meets this file holding all three or waiting for transactions, never the half of a
-- cycle. CREATE OR REPLACE FUNCTION takes no table lock. Behind a long reader the file gives up with
-- 55P03 and changes nothing.
--
-- THE DOWN REFUSES WHILE A qr_live ROW EXISTS (00043, 00095's precedent): narrowing a set under a
-- row that holds the removed value is either a failing CHECK or a record silently out of its feed.

-- +goose Up
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.transactions, public.punch_events, public.site_punches IN ACCESS EXCLUSIVE MODE;

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION -- the bodies and sets this file replaces are the ones it read.
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.live_qr_displays') IS NULL THEN
        RAISE EXCEPTION 'migration 00112 (M16-19) needs migration 00111 (live_qr_displays). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- 00067's two bodies and 00043's gate, byte for byte (00049's / 00067's md5 precedent): a trigger
    -- changed by any other hand would be overwritten silently.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.punch_feed()'::pg_catalog.regprocedure) IS DISTINCT FROM '8878c609a1e745c3753dffe7730bad9a'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.holding_feed()'::pg_catalog.regprocedure) IS DISTINCT FROM '2043e7eca5758c33974bcb0a8785d493'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.holding_site_gate()'::pg_catalog.regprocedure) IS DISTINCT FROM '032199e94329be7ce926b77746695ec8' THEN
        RAISE EXCEPTION 'migration 00112 (M16-19) replaces punch_feed, holding_feed and holding_site_gate, and one of their bodies is not the one it was written against (00067''s two, 00043''s gate). Another change reached it first: rebase this file on it. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- No set already names the value: then another hand widened it, and this file would hide that.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                WHERE c.conrelid IN ('public.transactions'::pg_catalog.regclass, 'public.punch_events'::pg_catalog.regclass,
                                     'public.site_punches'::pg_catalog.regclass)
                  AND c.contype = 'c' AND pg_catalog.pg_get_constraintdef(c.oid) LIKE '%qr_live%') THEN
        RAISE EXCEPTION 'migration 00112 (M16-19) adds qr_live to the channel CHECKs, and one already names it. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The five closed sets, NOT VALID (00113 validates four).
-- ---------------------------------------------------------------------------
ALTER TABLE transactions
    DROP CONSTRAINT transactions_channel_check,
    DROP CONSTRAINT transactions_site_by_channel,
    DROP CONSTRAINT transactions_shift_snapshot_shape,
    ADD CONSTRAINT transactions_channel_check
        CHECK (channel IN ('nfc', 'qr', 'qr_live', 'manual')) NOT VALID,
    -- The third arm: a live QR refusal names the screen's location, and no plaque.
    ADD CONSTRAINT transactions_site_by_channel
        CHECK (site_tenant_id IS NULL
               OR (channel IN ('nfc', 'qr') AND site_tag_uid IS NOT NULL)
               OR (channel = 'manual' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)
               OR (channel = 'qr_live' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)) NOT VALID,
    ADD CONSTRAINT transactions_shift_snapshot_shape CHECK (
        (shift_source IS NULL
            AND shift_start IS NULL AND shift_end IS NULL AND shift_overnight IS NULL AND shift_timezone IS NULL)
        OR (shift_source IS NOT NULL AND shift_source = 'none' AND type IS NOT NULL AND channel IN ('nfc', 'qr', 'qr_live')
            AND shift_start IS NULL AND shift_end IS NULL AND shift_overnight IS NULL AND shift_timezone IS NULL)
        OR (shift_source IS NOT NULL AND shift_source IN ('department', 'location') AND type IS NOT NULL AND channel IN ('nfc', 'qr', 'qr_live')
            AND shift_start IS NOT NULL AND shift_end IS NOT NULL AND shift_overnight IS NOT NULL AND shift_timezone IS NOT NULL)
    ) NOT VALID;

ALTER TABLE punch_events
    DROP CONSTRAINT punch_events_channel,
    ADD CONSTRAINT punch_events_channel CHECK (channel IN ('nfc', 'qr', 'qr_live')) NOT VALID;

ALTER TABLE site_punches
    DROP CONSTRAINT site_punches_channel_check,
    ADD CONSTRAINT site_punches_channel_check CHECK (channel IN ('nfc', 'qr', 'qr_live', 'manual')) NOT VALID;

-- ---------------------------------------------------------------------------
-- 2. punch_feed -- 00067's body; the two gates and the source rule name qr_live. Nothing else moves.
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
        IF NEW.verdict <> 'ok' OR NEW.channel NOT IN ('nfc', 'qr', 'qr_live') OR NEW.practice IS NOT FALSE OR NEW.type IS NULL THEN
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
        IF v_verdict <> 'flag' OR v_channel NOT IN ('nfc', 'qr', 'qr_live') OR v_practice IS NOT FALSE OR v_type IS NULL THEN
            RETURN NULL;
        END IF;
    END IF;

    -- Who chose the direction (M15-13): ADR 0032 §B.4's reader rule, 00067's header.
    v_dsrc := CASE WHEN v_channel IN ('nfc', 'qr', 'qr_live') AND v_type IS NOT NULL THEN CASE WHEN v_stored IS NULL THEN 'auto' WHEN v_stored IN ('auto', 'employee') THEN v_stored END END;

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
-- 3. holding_feed -- 00067's body; the two event gates and the source rule name qr_live. A holding
-- live QR tap is a refusal (Q43 (b)), so its gates never pass today; they name the channel so that
-- the trigger is a reader of the vocabulary, not of a policy (opening the holding is a decision with
-- its own ADR -- ADR 0027 §2 -- not a forgotten list).
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
        v_event := NEW.verdict = 'ok' AND NEW.channel IN ('nfc', 'qr', 'qr_live') AND NEW.practice IS FALSE AND NEW.type IS NOT NULL;
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
        v_event := NEW.outcome = 'approved' AND v_verdict = 'flag' AND v_channel IN ('nfc', 'qr', 'qr_live') AND v_practice IS FALSE
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
        v_dsrc := CASE WHEN v_channel IN ('nfc', 'qr', 'qr_live') AND v_type IS NOT NULL THEN CASE WHEN v_stored IS NULL THEN 'auto' WHEN v_stored IN ('auto', 'employee') THEN v_stored END END;

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
-- 4. holding_site_gate -- 00043's body; the tolerant arm names qr_live, so a live QR REFUSAL at a
-- peer's screen is written within 60 s after the membership ends (00043's header: "every refusal on
-- a tap channel"), like a static QR refusal -- a membership ending between the decision and the write
-- does not cost the record (§4.6). A qr_live row that is not a refusal takes the strict arm.
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_site_gate()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
DECLARE
    v_advanced boolean;
    v_tolerant boolean;
    v_peer     boolean;
BEGIN
    IF TG_RELID IS DISTINCT FROM 'public.transactions'::pg_catalog.regclass
       OR TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'holding_site_gate: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NEW.site_tenant_id IS NULL THEN
        RETURN NEW;
    END IF;

    PERFORM public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'lock');

    v_advanced := NEW.ctr IS NOT NULL AND EXISTS (
        SELECT 1
          FROM public.tags AS t
         WHERE t.uid = NEW.site_tag_uid
           AND t.tenant_id = NEW.site_tenant_id
           AND t.last_ctr >= NEW.ctr);
    v_tolerant := NEW.channel IN ('nfc', 'qr', 'qr_live')
                  AND ((NEW.channel = 'nfc' AND NEW.sun_valid IS TRUE AND v_advanced)
                       OR (NEW.verdict = 'reject' AND (NEW.sun_valid IS NOT TRUE OR v_advanced)));

    IF v_tolerant THEN
        v_peer := public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'tolerant_stmt');
    ELSE
        v_peer := public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'strict_stmt');
    END IF;
    IF v_peer IS NOT TRUE THEN
        RAISE EXCEPTION 'holding_site_gate: the site of this row is not a peer of its tenant'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'holding_site_gate';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION -- the owners, the definer flag and the EXECUTE grants are unchanged (CREATE OR
-- REPLACE keeps them; measured as the roles meet them).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.pg_get_userbyid((SELECT proowner FROM pg_catalog.pg_proc WHERE oid = 'public.punch_feed()'::pg_catalog.regprocedure)) <> 'tappa_feedwriter'
       OR pg_catalog.pg_get_userbyid((SELECT proowner FROM pg_catalog.pg_proc WHERE oid = 'public.holding_feed()'::pg_catalog.regprocedure)) <> 'tappa_holdingdefiner'
       OR pg_catalog.pg_get_userbyid((SELECT proowner FROM pg_catalog.pg_proc WHERE oid = 'public.holding_site_gate()'::pg_catalog.regprocedure)) <> 'tappa_holdingdefiner'
       OR pg_catalog.has_function_privilege('tappa_app', 'public.punch_feed()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_feed()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_site_gate()', 'EXECUTE')
       OR (SELECT count(*) FROM pg_catalog.pg_constraint c
            WHERE c.conrelid IN ('public.transactions'::pg_catalog.regclass, 'public.punch_events'::pg_catalog.regclass,
                                 'public.site_punches'::pg_catalog.regclass)
              AND c.contype = 'c' AND pg_catalog.pg_get_constraintdef(c.oid) LIKE '%qr_live%') <> 5 THEN
        RAISE EXCEPTION 'migration 00112 (M16-19): the three triggers must keep their owners and nobody but them execute them, and exactly five CHECKs name qr_live. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD NARROW A SET UNDER A ROW REFUSES (CLAUDE.md §4.3, §4.6). While a transactions,
-- punch_events or site_punches row carries qr_live this Down stops and changes nothing: the CHECKs
-- could not be added back over it, and a punch_feed that no longer names the channel would leave the
-- next approval of such a row out of the feed. After the first live QR tap the way back is the
-- binary, not the schema. The three tables are LOCKED BEFORE THEY ARE LOOKED AT (between an unlocked
-- EXISTS and the ALTER a tap could commit one), in the Up's order.
--
-- The CHECKs come back by 00005's, 00043's, 00046's, 00048's and 00095's texts, and VALID where they
-- were (all but the shift snapshot's, which 00095 left NOT VALID): the inverse of this file is the
-- state it found. Re-adding a validated CHECK scans its table under the ACCESS EXCLUSIVE this Down
-- holds anyway -- the cost of a Down, which runs only when nothing qr_live exists.
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.transactions, public.punch_events, public.site_punches IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.transactions WHERE channel = 'qr_live')
       OR EXISTS (SELECT 1 FROM public.punch_events WHERE channel = 'qr_live')
       OR EXISTS (SELECT 1 FROM public.site_punches WHERE channel = 'qr_live') THEN
        RAISE EXCEPTION 'migration 00112 Down: transactions, punch_events or site_punches holds a qr_live row (a live QR tap, its feed event or its site projection); records are immutable and the channel sets cannot narrow under them. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The bodies the Down gives back over must be this file's: a later hand's change would be lost.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.punch_feed()'::pg_catalog.regprocedure) IS DISTINCT FROM '1bda5aeed5b21a4bc7f83358cc980c9c'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.holding_feed()'::pg_catalog.regprocedure) IS DISTINCT FROM '2320bcb86611f56737851c1fc4c4eb9f'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.holding_site_gate()'::pg_catalog.regprocedure) IS DISTINCT FROM '3c2a06f02032eaa251c299e8730f8033' THEN
        RAISE EXCEPTION 'migration 00112 Down: punch_feed, holding_feed or holding_site_gate is not the body 00112 installed. Another change reached it after this file: walk that one back first. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE site_punches
    DROP CONSTRAINT site_punches_channel_check,
    ADD CONSTRAINT site_punches_channel_check CHECK (channel IN ('nfc', 'qr', 'manual'));

ALTER TABLE punch_events
    DROP CONSTRAINT punch_events_channel,
    ADD CONSTRAINT punch_events_channel CHECK (channel IN ('nfc', 'qr'));

ALTER TABLE transactions
    DROP CONSTRAINT transactions_channel_check,
    DROP CONSTRAINT transactions_site_by_channel,
    DROP CONSTRAINT transactions_shift_snapshot_shape,
    ADD CONSTRAINT transactions_channel_check
        CHECK (channel IN ('nfc', 'qr', 'manual')),
    ADD CONSTRAINT transactions_site_by_channel
        CHECK (site_tenant_id IS NULL
               OR (channel IN ('nfc', 'qr') AND site_tag_uid IS NOT NULL)
               OR (channel = 'manual' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)),
    ADD CONSTRAINT transactions_shift_snapshot_shape CHECK (
        (shift_source IS NULL
            AND shift_start IS NULL AND shift_end IS NULL AND shift_overnight IS NULL AND shift_timezone IS NULL)
        OR (shift_source IS NOT NULL AND shift_source = 'none' AND type IS NOT NULL AND channel IN ('nfc', 'qr')
            AND shift_start IS NULL AND shift_end IS NULL AND shift_overnight IS NULL AND shift_timezone IS NULL)
        OR (shift_source IS NOT NULL AND shift_source IN ('department', 'location') AND type IS NOT NULL AND channel IN ('nfc', 'qr')
            AND shift_start IS NOT NULL AND shift_end IS NOT NULL AND shift_overnight IS NOT NULL AND shift_timezone IS NOT NULL)
    ) NOT VALID;

-- 00067's two bodies and 00043's gate, byte for byte (the Up's precondition pins their md5).
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_site_gate()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
DECLARE
    v_advanced boolean;
    v_tolerant boolean;
    v_peer     boolean;
BEGIN
    IF TG_RELID IS DISTINCT FROM 'public.transactions'::pg_catalog.regclass
       OR TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'holding_site_gate: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NEW.site_tenant_id IS NULL THEN
        RETURN NEW;
    END IF;

    PERFORM public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'lock');

    v_advanced := NEW.ctr IS NOT NULL AND EXISTS (
        SELECT 1
          FROM public.tags AS t
         WHERE t.uid = NEW.site_tag_uid
           AND t.tenant_id = NEW.site_tenant_id
           AND t.last_ctr >= NEW.ctr);
    v_tolerant := NEW.channel IN ('nfc', 'qr')
                  AND ((NEW.channel = 'nfc' AND NEW.sun_valid IS TRUE AND v_advanced)
                       OR (NEW.verdict = 'reject' AND (NEW.sun_valid IS NOT TRUE OR v_advanced)));

    IF v_tolerant THEN
        v_peer := public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'tolerant_stmt');
    ELSE
        v_peer := public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'strict_stmt');
    END IF;
    IF v_peer IS NOT TRUE THEN
        RAISE EXCEPTION 'holding_site_gate: the site of this row is not a peer of its tenant'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'holding_site_gate';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
