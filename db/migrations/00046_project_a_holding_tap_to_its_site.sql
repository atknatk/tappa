-- 00046 -- M11 API-1c: the site's view of a tap on its plaque. A member of a holding (the
-- EMPLOYER) records its employee's tap on another member's plaque (the SITE) in its own
-- transactions (00043); the site sees that tap through a closed PROJECTION, site_punches, written
-- by the projection half of the one holding trigger, holding_feed (AFTER INSERT ON transactions
-- and ON transaction_reviews -- API-2 adds the counters and the events to the same function).
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §9 (the table, its
-- columns, the closed outcome set and its TOTAL mapping, the two partial unique indexes and the
-- server-clock bucket, tappa_app's column list without sun_valid), §2 (the contract every holding
-- trigger keeps; the closed list -- holding_feed is its row), §3 (the definer's matrix: the
-- written table's columns, the conflict target's SELECT, the transactions / employees rows the
-- approval path reads; the "never SELECT" list -- policy_context is NOT granted, the trigger reads
-- one key of NEW's), §6 (the transactions arm does not re-ask the membership: a row the BEFORE gate
-- accepted is a peer's; approvals ask 'strict_stmt'), §8 (the trigger is FIRST in name order);
-- K-1h (the site sees the outcome, never the reason), K-1i (ctr_gap from NEW's tap:ctrGap);
-- CLAUDE.md §4.3 (site_punches is append-only; transactions is not touched), §4.5, §4.6 (no branch
-- rolls the employer's INSERT back: 'ignored' is an explicit "not written"), §6.
--
-- WHAT IT DOES, IN ORDER:
--   0. the precondition: 00043/00045 in place, none of this file's objects, and
--      tappa_holdingdefiner reaching none of the tables this file grants it;
--   1. site_punches with row level security's five, append-only (row + TRUNCATE guards);
--   2. tappa_app's column-listed SELECT -- sun_valid NOT in it (K-1h) -- and nothing else;
--   3. tappa_holdingdefiner's grants: INSERT on the projection's columns, SELECT on the two
--      conflict targets (a targeted ON CONFLICT needs it -- measured, below), and the approval
--      path's reads of transactions and employees;
--   4. holding_feed and its two triggers;
--   5. the postcondition.
--
-- §4.5 -- CROSSED ON PURPOSE, IN THE PLACE ADR 0027 ALLOWS. holding_feed belongs to
-- tappa_holdingdefiner (BYPASSRLS): it writes a row into the SITE's tenant from the EMPLOYER's
-- INSERT. The row's tenant is NEW.site_tenant_id and nothing else (asserted); the site is a peer
-- because the BEFORE gate (holding_site_gate, 00043) let the row exist -- the transactions arm
-- does not ask again (ADR 0027 §6: "BEFORE kabul edip AFTER'ın KF tarafını atladığı satır yok").
-- An approval asks holding_peer 'strict_stmt' and, past the membership, writes nothing for the
-- site (the named cost of §13 item 7) -- never an error.
--
-- 🔴 §4.6: NO BRANCH RAISES ONCE THE BINDING IS RIGHT. The mapping is a closed CASE over
-- transactions' closed verdict and channel sets (00005's CHECKs), 'ignored' is the explicit "not
-- written" branch, and the two refusal inserts are TARGETED ON CONFLICT DO NOTHING -- a duplicate
-- refusal is dropped at the site and still written at the employer. The one wait is the unique
-- index's on a concurrent identical refusal not yet committed, bounded by the function's
-- lock_timeout (ADR 0027 §8's named cost). SitePunches_OutcomeMappingIsTotal walks every cell.
--
-- MEASURED (a throwaway PostgreSQL 17 container, 2026-10-14):
--   * a TARGETED `ON CONFLICT (<cols>) WHERE <predicate> DO NOTHING` needs SELECT on the target
--     columns AND on the predicate's columns: without SELECT (sun_valid) the insert was refused
--     42501 (ADR 0027 §3, "çakışma hedefi" -- the list is the seven columns below);
--   * NULLS NOT DISTINCT lets a refusal with no employee and a NULL sun_valid collide with its
--     twin (three inserts, one row);
--   * date_trunc('hour', now(), 'UTC') is the same instant in a UTC, a Europe/Malta and an
--     Asia/Kolkata session; the two-argument form moved by 30 minutes in the last (a whole-hour
--     zone such as Malta cannot tell the two forms apart -- SitePunches_BucketIsServerClockAndZoneFree
--     uses all three).
--
-- SECRETS: none. The projection carries no coordinate, no address, no note, no policy context, no
-- verdict, no matched rule and no e-mail (SitePunches_CatalogCarriesNoEvidenceColumns).
-- RETENTION: kept indefinitely until the user's DPA note names a period (ADR 0027 §13 item 4); no
-- delete path exists -- the table is append-only.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_site_gate()') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_advance_tag_counter(character, integer)') IS NULL
       OR pg_catalog.pg_get_function_result('public.holding_advance_tag_counter(character, integer)'::pg_catalog.regprocedure)
          <> 'TABLE(outcome text, tag_status text, ctr_gap integer)' THEN
        RAISE EXCEPTION 'migration 00046 (M11 API-1c) needs migrations 00043 and 00045 (the site columns, the site gate, the counter gap). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.site_punches') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_feed')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g
                   WHERE g.tgname IN ('transactions_holding_feed', 'transaction_reviews_holding_feed')) THEN
        RAISE EXCEPTION 'migration 00046 (M11 API-1c) creates site_punches and holding_feed, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- A column grant cannot narrow a wider privilege some other hand gave (00024 measured it).
    IF pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.transactions', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.transactions', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.transactions', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.employees', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.employees', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.employees', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.transactions', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.employees', 'DELETE') THEN
        RAISE EXCEPTION 'migration 00046 (M11 API-1c) gives tappa_holdingdefiner column privileges on transactions and employees, and the role already reaches one of them. A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. site_punches (ADR 0027 §9) -- in the SITE's tenant.
-- ---------------------------------------------------------------------------
-- One row per tap on this tenant's plaque (or manual record at its venue) that a PEER's employee
-- made -- the employer's record stays the employer's. What the site may know (K-1d, K-1f, K-1h):
-- who (the employer's code, the employee's id, ref, code and name -- a snapshot at the tap),
-- where (its own venue and plaque), when (the declared occurred_at; the server's created_at and
-- the hour bucket), which way (type) and the OUTCOME -- never the reason (no verdict, no matched
-- rule, no policy context), never evidence (no GPS, IP or note), never an e-mail.
--
--   transaction_id   the employer's row: the site cannot read it (RLS); it is the key the "last
--                    outcome" fold groups by, and the key -- with the employer -- of the FK below
--   sun_valid        kept for the refusal dedup's split, NOT selectable by tappa_app (K-1h:
--                    "refused and sun_valid" would say "the signature was good")
--   bucket_hour      date_trunc('hour', now(), 'UTC') -- the server's hour, whatever the session
--                    zone, never the client's occurred_at or a writable created_at
--   ctr_gap          K-1i: the employer's tap:ctrGap of this tap (NULL when it carries none)
--   created_at       the server's clock: the site's "last seen" and its lost-plaque alert read it
--                    (ADR 0027 §9, 11th round: a declared occurred_at of 2099 must move nothing)
CREATE TABLE site_punches (
    tenant_id          uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id                 uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    transaction_id     uuid        NOT NULL,
    employer_tenant_id uuid        NOT NULL,
    employer_api_code  text,
    employee_id        uuid,
    employee_ref       text,
    employee_code      text,
    employee_name      text,
    site_location_id   uuid,
    site_tag_uid       character(14),
    ctr                integer,
    sun_valid          boolean,
    bucket_hour        timestamptz NOT NULL,
    occurred_at        timestamptz NOT NULL,
    channel            text        NOT NULL CHECK (channel IN ('nfc', 'qr', 'manual')),
    type               text        CHECK (type IN ('in', 'out')),
    outcome            text        NOT NULL CHECK (outcome IN ('counted', 'pending_review', 'approved', 'rejected',
                                                               'refused_tag_not_active', 'refused', 'employer_manual')),
    ctr_gap            integer,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT site_punches_is_another_tenant CHECK (employer_tenant_id <> tenant_id),
    CONSTRAINT site_punches_bucket_is_an_hour CHECK (bucket_hour = date_trunc('hour', bucket_hour, 'UTC')),
    -- the employer's row: an append-only table that names it cannot point at nothing
    CONSTRAINT site_punches_transaction_fk FOREIGN KEY (transaction_id, employer_tenant_id)
        REFERENCES transactions (id, tenant_id) ON DELETE RESTRICT,
    -- the site's own venue and plaque (a location delete's belt beside transactions' -- §9)
    CONSTRAINT site_punches_location_fk FOREIGN KEY (site_location_id, tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT site_punches_tag_fk FOREIGN KEY (site_tag_uid, tenant_id)
        REFERENCES tags (uid, tenant_id) ON DELETE RESTRICT
);

-- The panel's reads: the newest first; a plaque's last seen; a venue's references.
CREATE INDEX site_punches_tenant_created_idx ON site_punches (tenant_id, created_at DESC);
CREATE INDEX site_punches_tenant_tag_idx ON site_punches (tenant_id, site_tag_uid, created_at DESC);
CREATE INDEX site_punches_tenant_location_idx ON site_punches (tenant_id, site_location_id);

-- 🔴 THE REFUSAL DEDUP (ADR 0027 §9; 9th-11th rounds). The employer writes EVERY refusal
-- (§4.6); the site gets at most one row per:
--   * a REAL refusal (sun_valid IS TRUE -- the counter really advanced, e.g. a deactivated
--     employee's tap): plaque + counter + employee;
--   * every OTHER refusal (a replay, a forged MAC, a QR refusal -- the row's ctr is whatever the
--     URL said, so it is not a key): plaque + employee + outcome + the server's hour. outcome is
--     in the key so a forged 'refused' cannot suppress a lost plaque's alert in the same hour.
-- NULLS NOT DISTINCT closes the holes a NULL employee or a NULL sun_valid would open.
CREATE UNIQUE INDEX site_punches_real_refusal_once
    ON site_punches (tenant_id, site_tag_uid, ctr, employee_id) NULLS NOT DISTINCT
    WHERE outcome LIKE 'refused%' AND sun_valid IS TRUE;
CREATE UNIQUE INDEX site_punches_other_refusal_hourly
    ON site_punches (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour) NULLS NOT DISTINCT
    WHERE sun_valid IS NOT TRUE AND outcome LIKE 'refused%';

ALTER TABLE site_punches ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_punches FORCE ROW LEVEL SECURITY;
CREATE POLICY site_punches_tenant_isolation ON site_punches
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- APPEND-ONLY (CLAUDE.md §4.3's treatment, 00005 + 00021): the row trigger stops UPDATE and DELETE
-- for every role, the superuser included; the statement trigger stops TRUNCATE.
CREATE TRIGGER site_punches_no_mutation
    BEFORE UPDATE OR DELETE ON site_punches
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();
CREATE TRIGGER site_punches_no_truncate
    BEFORE TRUNCATE ON site_punches
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- ---------------------------------------------------------------------------
-- 2. tappa_app -- reads the projection, column by column, sun_valid NOT among them (K-1h).
-- ---------------------------------------------------------------------------
-- The `arwd` rule (ADR 0028 §1): production's default hands a new table all four verbs, so ALL is
-- revoked first. No INSERT: the trigger is the one writer.
REVOKE ALL ON site_punches FROM tappa_app;
GRANT SELECT (tenant_id, id, transaction_id, employer_tenant_id, employer_api_code, employee_id, employee_ref,
              employee_code, employee_name, site_location_id, site_tag_uid, ctr, bucket_hour, occurred_at,
              channel, type, outcome, ctr_gap, created_at)
    ON site_punches TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. tappa_holdingdefiner (ADR 0027 §3) -- each column one of the paths reads or writes
-- (TestHoldingDefiner_EveryGrantIsUsed revokes them one at a time).
-- ---------------------------------------------------------------------------
-- site_punches: the projection's columns (id and created_at take their defaults), and SELECT on
-- the two conflict targets -- their columns and their predicates' (measured above).
GRANT INSERT (tenant_id, transaction_id, employer_tenant_id, employer_api_code, employee_id, employee_ref,
              employee_code, employee_name, site_location_id, site_tag_uid, ctr, sun_valid, bucket_hour,
              occurred_at, channel, type, outcome, ctr_gap)
    ON site_punches TO tappa_holdingdefiner;
GRANT SELECT (tenant_id, site_tag_uid, ctr, employee_id, outcome, bucket_hour, sun_valid)
    ON site_punches TO tappa_holdingdefiner;
-- transactions: the approval path reads the decided row's shape (an approval's NEW carries only
-- its ids). gps_*, source_ip, note and policy_context stay on K-34's "never SELECT" list -- the
-- transactions arm reads NEW, which needs no privilege.
GRANT SELECT (id, tenant_id, employee_id, site_tenant_id, site_location_id, site_tag_uid, channel, type,
              occurred_at, ctr, sun_valid)
    ON transactions TO tappa_holdingdefiner;
-- employees: the snapshot of who tapped (K-1f) -- never the e-mail.
GRANT SELECT (id, tenant_id, external_ref, employee_code, full_name) ON employees TO tappa_holdingdefiner;

-- ===========================================================================
-- 4. holding_feed -- the projection half of the one holding trigger (ADR 0027 §2, §8, §9).
-- ===========================================================================
-- AFTER INSERT ON transactions (a row that names a site) and ON transaction_reviews (a decision
-- on one). In order:
--   transactions arm   the outcome from NEW -- the closed mapping below; 'ignored' is not written;
--                      no membership question (the gate's acceptance IS the peer answer)
--   reviews arm        the decided row read by its key, explicit tenant; not a site row -> nothing;
--                      holding_peer 'strict_stmt' -> past the membership nothing for the site
--   both               the employer's snapshot (its code, the employee's ref, code and name); then
--                      ONE insert into the SITE's tenant: a refusal by its dedup target, anything
--                      else plainly
-- The mapping (ADR 0027 §9 -- "tam ve toplam"; verdict x channel x review):
--   nfc/qr  ok -> counted   flag -> pending_review   reject -> refused_tag_not_active when the
--           guardrail that decided was sys:tag-not-active, else refused   ignored -> NOT WRITTEN
--   manual  ok / flag / reject -> employer_manual                        ignored -> NOT WRITTEN
--   review  approved -> approved   rejected -> rejected (carrying the decided row's occurred_at,
--           type, channel and counter)
-- K-1i: ctr_gap is the integer NEW.policy_context -> 'tap:ctrGap' when it is one (0 <= gap <
-- 2^24, whole), NULL otherwise -- read once, and that key is all this body reads of the context.
-- API-2 adds the counter lock and the two event rows to THIS function (K-31: before the rows);
-- the projection stays K-4-independent (§8).
-- +goose StatementBegin
CREATE FUNCTION public.holding_feed()
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
ALTER FUNCTION public.holding_feed() OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_feed() FROM PUBLIC;

-- FIRST IN NAME ORDER among each table's AFTER INSERT row triggers (ADR 0027 §8, K-31; ADR 0028
-- §4: the own-tenant trigger of API-2 is named after it). Only a row that names a site reaches it
-- on transactions; every approval reaches it (its NEW carries no site -- the body looks it up).
CREATE TRIGGER transactions_holding_feed
    AFTER INSERT ON transactions
    FOR EACH ROW
    WHEN (NEW.site_tenant_id IS NOT NULL)
    EXECUTE FUNCTION public.holding_feed();
CREATE TRIGGER transaction_reviews_holding_feed
    AFTER INSERT ON transaction_reviews
    FOR EACH ROW
    EXECUTE FUNCTION public.holding_feed();

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION -- measured as the roles would meet it (has_* count membership and PUBLIC).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.site_punches', 'sun_valid', 'SELECT')
       OR NOT pg_catalog.has_column_privilege('tappa_app', 'public.site_punches', 'outcome', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.site_punches', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.site_punches', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.site_punches', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.site_punches', 'TRUNCATE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_feed()', 'EXECUTE')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'policy_context', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00046 (M11 API-1c): tappa_app must read site_punches without sun_valid, write nothing there and not EXECUTE holding_feed, and the definer must not read policy_context -- through PUBLIC or a role membership one of these does not hold. Fix the grants by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- 🔴 A Down THAT WOULD ERASE THE SITE'S VIEW OF A RECORDED TAP REFUSES (CLAUDE.md §4.3, §4.6; the
-- 00043 precedent). site_punches is append-only and holds what the site was told -- its dedup and
-- its server clock cannot be rebuilt from the employer's rows. While it holds a row this Down stops
-- and changes nothing; on a database with none it takes the triggers, the function, the grants and
-- the table away.
-- ⚠️ It takes ACCESS EXCLUSIVE on transactions and transaction_reviews (DROP TRIGGER; the FK's
-- triggers) and on locations, tags and tenants (DROP TABLE takes the referenced tables' RI
-- triggers): opAtVersion skips this file (no operator object), and the holding tests' Down walks
-- lock holdingDownTables first.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.site_punches) THEN
        RAISE EXCEPTION 'migration 00046 Down: site_punches holds rows (the sites'' view of taps on their plaques); dropping it would erase them, and the projection is append-only. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS transaction_reviews_holding_feed ON transaction_reviews;
DROP TRIGGER IF EXISTS transactions_holding_feed ON transactions;
DROP FUNCTION IF EXISTS public.holding_feed();
REVOKE SELECT (id, tenant_id, external_ref, employee_code, full_name) ON employees FROM tappa_holdingdefiner;
REVOKE SELECT (id, tenant_id, employee_id, site_tenant_id, site_location_id, site_tag_uid, channel, type,
               occurred_at, ctr, sun_valid)
    ON transactions FROM tappa_holdingdefiner;
DROP TABLE IF EXISTS site_punches;
