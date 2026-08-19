-- Tappa demo/test seed -- master data only (see skill tappa-seed, M1-10).
--
-- WHAT THIS IS
--   The two real design partners, structurally faithful:
--     * Kebab Factory Ltd.        -- multi-location, 9 locations, no departments
--     * Kebab Manufacturing Co.   -- single-location, 5 departments
--   Plus their employees and wall-mounted NFC plaques (tags). Dashboards,
--   integration tests and sales demos all read this exact data, so it must stay
--   consistent -- inconsistent demo data makes a broken engine look correct.
--
-- WHAT THIS IS NOT
--   No transactions, no audit_log, no transaction_reviews. Tap records are the
--   decision engine's OUTPUT and are produced by M5-09 ("simulate a day") from
--   the engine itself, never hand-authored here (section 5). ONE admin_users owner
--   per tenant IS seeded (M1-11) so the panel demo can log in; no managers, no
--   admin_sessions, no password_resets (those are runtime artifacts of M6/M7).
--   No employee sessions: a persistent session is created by the activation flow
--   (M2) / day simulation (M5-09), not part of the durable master identity graph.
--
-- IDEMPOTENCY
--   Every row uses a fixed UUID / tag UID and ON CONFLICT DO NOTHING. Running
--   `make seed` twice is clean: the second run inserts nothing and errors on
--   nothing. There is no DELETE -- ON CONFLICT is the whole idempotency story.
--   ONE EXCEPTION, scoped to a single column: locations carries a DO UPDATE that
--   backfills wifi_ssid (00010) into rows seeded before that column existed. It
--   is guarded so it fires at most once per row and never overwrites a value;
--   the reasoning is written out at that statement.
--   The same fixed identifiers are mirrored, typed, in test/fixtures/ids.go so
--   Go tests reference named handles instead of magic strings.
--   ONE MORE THING TO KNOW, and it belongs to scripts/seed.sh rather than to this
--   file: the key-wrapping half rewrites all twelve tags.aes_key_ref values on
--   EVERY run, because sun.Wrap draws a fresh GCM nonce each time. The bytes
--   differ between runs; the keys they decrypt to do not. Row identity is still
--   idempotent -- nothing is inserted, deleted or otherwise changed -- and the
--   reason it is not byte-stable is written out at that statement: the stable
--   alternative would strand a database after a KEK change.
--
-- TENANT ISOLATION GROUND
--   No single row is shared between the two tenants. Every KF id starts with
--   1..., every KM id starts with 2... -- visually and structurally disjoint.
--   This is the ground truth the RLS isolation test (M1-09) stands on.
--
-- TIMEZONE POLICY (section 6 -- UTC in the DB; this is where night-shift bugs are born)
--   * Shift columns are `time` = LOCAL Malta wall-clock, stored verbatim. Date +
--     timezone combination happens in the domain/render layer, NEVER here. So
--     Rusty Bar 18:00-02:00 is written as TIME '18:00' / TIME '02:00' as-is.
--   * timestamptz columns are UTC. now() already returns UTC, so lifecycle
--     stamps with no wall-clock meaning (created_at, invited_at) are plain
--     now()-relative intervals -- never a hard-coded calendar date, so the
--     dashboard never goes empty a week later.
--   * Where a stamp DOES mean a local wall-clock moment (activated_at at ~09:30
--     Malta, deactivated_at at ~17:00 Malta) the Malta->UTC conversion is done
--     EXPLICITLY with AT TIME ZONE 'Europe/Malta': it reads the naive wall time
--     AS Malta local and returns the UTC instant, DST-aware, with no hard-coded
--     +1/+2 offset.
--
-- SECRETS (section 4.7)
--   tags.aes_key_ref is written TWICE and this file writes only the first half.
--   Here it gets a loud ASCII PLACEHOLDER; scripts/seed.sh then runs
--   test/fixtures/seedkeys, which replaces every one of them with a genuine
--   AES-256-GCM envelope (nonce||ciphertext||tag = 44 bytes) sealed under the
--   operator's TAPPA_TAG_KEK. Two reasons the wrapped value cannot be a literal
--   here: it depends on a KEK that lives in the environment and never in the repo,
--   and sun.Wrap draws a fresh nonce on every call, so there is no single "the"
--   value even for one KEK. The plaintext per-tag keys are DERIVED from a public,
--   deliberately loud label (fixtures.SeedTagKeyLabel + the uid), so every demo
--   key is recomputable by anyone reading the repo -- which is precisely what
--   makes them self-evidently fake. Real NTAG AES keys never live here.
--   ⚠️ RUNNING THIS FILE ALONE (psql < seed.sql) LEAVES BROKEN PLAQUES: the
--   placeholder below is 44 bytes (30-character label + 14 hex uid; measured with
--   octet_length) and is the RIGHT LENGTH but the WRONG BYTES -- sun.Unwrap gets
--   past its length check and fails GCM authentication instead, so every NFC tap
--   on such a plaque still answers 500. Use `make seed` / scripts/seed.sh, which
--   runs both halves; the second half also RAISEs if any demo plaque was left
--   behind.
--   🔴 THE LENGTH IS NO LONGER FREE, AND THAT IS WHY THE LABEL WAS SHORTENED
--   (migration 00021, backlog T7). It used to be 50 bytes and the schema had no
--   opinion; tags_aes_key_ref_is_kek_envelope now demands octet_length = 44
--   (ADR 0003 article 4: nonce 12 || ciphertext 16 || tag 16), so a 50-byte
--   placeholder makes this INSERT fail with 23514 and `make seed` dies at its
--   first half. Editing the label means COUNTING it: 30 characters, no more, no
--   less.
--   admin_users.password_hash is a bcrypt $2a$ hash of the DOCUMENTED dev-only
--   password "tappa-dev-only-changeme" (see the admin_users section below). Same
--   principle as the fake tag key: NOT a real secret, the demo login must work,
--   and no real customer credential ever lives in the repo. Production owners are
--   created by M8-07, never seeded.
--
-- NETWORK (section proof-of-place)
--   static_ips use only the documentation ranges 203.0.113.0/24 (KF) and
--   198.51.100.0/24 (KM). No real customer IP is ever written to the repo.

BEGIN;

-- --------------------------------------------------------------------- tenants
-- Direct INSERT ... VALUES: target column types are known, so uuid/text literals
-- coerce without explicit casts. vat_number is UNIQUE; the values are fake but
-- format-plausible (MT + 8 digits). Format/VIES validation is the app layer's job.
INSERT INTO tenants (id, name, vat_number, business_type, structure, plan, timezone, created_at)
VALUES
    ('10000000-0000-4000-8000-000000000001', 'Kebab Factory Ltd.',
     'MT20250001', 'restaurant', 'multi', 'founding', 'Europe/Malta',
     now() - interval '90 days'),
    ('20000000-0000-4000-8000-000000000001', 'Kebab Manufacturing Co. Ltd.',
     'MT20250002', 'production', 'single', 'founding', 'Europe/Malta',
     now() - interval '90 days')
ON CONFLICT (id) DO NOTHING;

-- ------------------------------------------------------------------- locations
-- KF: 9 locations, each its own static /32 from 203.0.113.0/24 (proof-of-place).
-- KM: 1 facility, one ISP-assigned /29 block from 198.51.100.0/24. GPS is Malta
-- and every pair is >= ~780 m apart, so the 150 m GPS-radius test is meaningful.
-- Shift times are Malta LOCAL wall-clock (see TIMEZONE POLICY). Rusty Bar carries
-- overnight = true WITH a filled 18:00-02:00 shift (shift_pair CHECK satisfied).
--
-- wifi_ssid (00010) is the network NAME the activation page asks the employee to
-- join (M5-02 phase B / Q14). It is display data and NOTHING decides on it -- the
-- IP half of proof-of-place stays static_ips above. Values follow one pattern,
-- <VENUE>-Staff, and stay ASCII on purpose: this file's naming discipline is
-- ASCII (see the Liam O'Brien note below) and the interesting case for
-- wifi_ssid -- multi-byte UTF-8 against a 32-OCTET limit -- is exercised where it
-- can be ASSERTED, in internal/db/locations_test.go, not by staring at a fixture.
-- The longest value here is 18 octets, comfortably inside the CHECK.
--
-- KF Msida deliberately has NO network name (NULL): a venue may simply not have
-- staff WiFi, and phase B must render that case -- the WiFi step is skipped and
-- later taps from that location fall back to the GPS path. It is the wifi_ssid
-- counterpart of Marco Bugeja's NULL email below: one fixture row that keeps the
-- nullable path from going untested by accident.
INSERT INTO locations
    (id, tenant_id, name, static_ips, gps_lat, gps_lng, shift_start, shift_end, overnight, wifi_ssid, created_at)
VALUES
    ('10000000-0000-4000-8000-000000000101', '10000000-0000-4000-8000-000000000001',
     'KF Hamrun',      ARRAY['203.0.113.10/32']::cidr[], 35.885000, 14.488000,
     TIME '10:00', TIME '22:00', false, 'KF-Hamrun-Staff',    now() - interval '80 days'),
    ('10000000-0000-4000-8000-000000000102', '10000000-0000-4000-8000-000000000001',
     'KF Mellieha',    ARRAY['203.0.113.11/32']::cidr[], 35.957000, 14.362000,
     TIME '10:00', TIME '22:00', false, 'KF-Mellieha-Staff',  now() - interval '80 days'),
    -- KF Msida: no staff network -- the NULL wifi_ssid fixture (see above).
    ('10000000-0000-4000-8000-000000000103', '10000000-0000-4000-8000-000000000001',
     'KF Msida',       ARRAY['203.0.113.12/32']::cidr[], 35.895000, 14.489000,
     TIME '10:00', TIME '22:00', false, NULL,                 now() - interval '80 days'),
    ('10000000-0000-4000-8000-000000000104', '10000000-0000-4000-8000-000000000001',
     'KF Valletta',    ARRAY['203.0.113.13/32']::cidr[], 35.899000, 14.514000,
     TIME '10:00', TIME '22:00', false, 'KF-Valletta-Staff',  now() - interval '80 days'),
    ('10000000-0000-4000-8000-000000000105', '10000000-0000-4000-8000-000000000001',
     'KF San Gwann',   ARRAY['203.0.113.14/32']::cidr[], 35.907000, 14.478000,
     TIME '10:00', TIME '22:00', false, 'KF-SanGwann-Staff',  now() - interval '80 days'),
    ('10000000-0000-4000-8000-000000000106', '10000000-0000-4000-8000-000000000001',
     'KF St Julians',  ARRAY['203.0.113.15/32']::cidr[], 35.918000, 14.489000,
     TIME '10:00', TIME '22:00', false, 'KF-StJulians-Staff', now() - interval '80 days'),
    ('10000000-0000-4000-8000-000000000107', '10000000-0000-4000-8000-000000000001',
     'KF Paceville',   ARRAY['203.0.113.16/32']::cidr[], 35.925000, 14.490000,
     TIME '11:00', TIME '23:00', false, 'KF-Paceville-Staff', now() - interval '80 days'),
    -- Rusty Bar keeps its own brand here too: it is the one KF venue whose name
    -- carries no "KF" prefix, so its network does not either.
    ('10000000-0000-4000-8000-000000000108', '10000000-0000-4000-8000-000000000001',
     'Rusty Bar',      ARRAY['203.0.113.17/32']::cidr[], 35.949000, 14.413000,
     TIME '18:00', TIME '02:00', true,  'RustyBar-Staff',     now() - interval '80 days'),
    ('10000000-0000-4000-8000-000000000109', '10000000-0000-4000-8000-000000000001',
     'KF Headquarter', ARRAY['203.0.113.18/32']::cidr[], 35.890000, 14.470000,
     TIME '09:00', TIME '17:00', false, 'KF-HQ-Staff',        now() - interval '80 days'),
    -- KM single facility. Location shift is the general 09:00-17:00 baseline;
    -- department shifts (below) override it for staff who have a department.
    ('20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000001',
     'KM Plant',       ARRAY['198.51.100.0/29']::cidr[], 35.870000, 14.450000,
     TIME '09:00', TIME '17:00', false, 'KM-Plant-Staff',     now() - interval '80 days')
-- DELIBERATE DEVIATION from this file's DO NOTHING rule, and ONLY for wifi_ssid.
-- Every dev database that was seeded before 00010 already holds these ten rows,
-- so DO NOTHING would leave every network name NULL there for good and `make
-- seed` would silently not deliver the fixture it claims to (only the destructive
-- `make db-reset` would). The narrow backfill below fills a name in exactly once:
--   * `locations.wifi_ssid IS NULL` -- an operator's hand-set value is never
--     overwritten, and neither is a name once this has run;
--   * `EXCLUDED.wifi_ssid IS NOT NULL` -- KF Msida is MEANT to stay NULL, so it
--     is not "updated" from NULL to NULL on every run.
-- Together those keep the idempotency claim literal rather than approximate: a
-- second run inserts nothing AND updates nothing (measured -- INSERT 0 0). No
-- other column is touched here; everything else still relies on DO NOTHING.
ON CONFLICT (id) DO UPDATE
    SET wifi_ssid = EXCLUDED.wifi_ssid
    WHERE locations.wifi_ssid IS NULL
      AND EXCLUDED.wifi_ssid IS NOT NULL;

-- ----------------------------------------------------------------- departments
-- KM only (KF models the location as the operational unit -- no departments).
-- Each department carries its own shift; a filled shift overrides the location
-- baseline for the employee who belongs to it (section 5). Shifts are Malta local time.
INSERT INTO departments
    (id, tenant_id, location_id, name, shift_start, shift_end, overnight, created_at)
VALUES
    ('20000000-0000-4000-8000-000000000201', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', 'KM General',
     TIME '09:00', TIME '17:00', false, now() - interval '80 days'),
    ('20000000-0000-4000-8000-000000000202', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', 'KM Meat Production',
     TIME '05:00', TIME '13:00', false, now() - interval '80 days'),
    ('20000000-0000-4000-8000-000000000203', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', 'KM Warehouse & Supply',
     TIME '08:00', TIME '16:00', false, now() - interval '80 days'),
    ('20000000-0000-4000-8000-000000000204', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', 'KM Central Kitchen',
     TIME '06:00', TIME '14:00', false, now() - interval '80 days'),
    ('20000000-0000-4000-8000-000000000205', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', 'KM Pastry & Bakery',
     TIME '04:00', TIME '12:00', false, now() - interval '80 days')
ON CONFLICT (id) DO NOTHING;

-- ------------------------------------------------------ employees (KF, no dept)
-- 25 employees. St Julians is the reference crew (15 people, ~10 tap on a given
-- day): 13 active, 1 invited (Grace Attard -- practice-tap candidate, not yet
-- activated), 1 deactivated (Paul Spiteri -- the deactivated-attempt scenario).
-- Rusty Bar has 3 night-shift staff; every other location has 1. department_id
-- is always NULL for KF (location = operational unit). One St Julians worker
-- (Marco Bugeja) has a NULL email -- physically invited, exercises the nullable
-- + partial-unique email path. Crew is multinational (Maltese + others).
--
-- INSERT ... SELECT so the status-dependent timestamps are computed once. The
-- first VALUES row carries explicit ::uuid / ::citext casts to fix the column
-- types; later rows coerce. See TIMEZONE POLICY for the activated_at conversion.
INSERT INTO employees
    (id, tenant_id, location_id, department_id, full_name, role, email, status,
     invited_at, activated_at, deactivated_at, created_at)
SELECT
    e.id, e.tenant_id, e.location_id, e.department_id, e.full_name, e.role, e.email, e.status,
    -- invited_at: UTC, now()-relative. Freshly invited ~2d ago; the rest ~60d ago.
    CASE WHEN e.status = 'invited' THEN now() - interval '2 days'
         ELSE now() - interval '60 days' END,
    -- activated_at: EXPLICIT Malta-local -> UTC. Only active/deactivated activated.
    CASE WHEN e.status = 'invited' THEN NULL
         ELSE (((now() AT TIME ZONE 'Europe/Malta')::date - 58) + TIME '09:30')
              AT TIME ZONE 'Europe/Malta' END,
    -- deactivated_at: EXPLICIT Malta-local -> UTC. Only the deactivated row.
    CASE WHEN e.status = 'deactivated'
         THEN (((now() AT TIME ZONE 'Europe/Malta')::date - 10) + TIME '17:00')
              AT TIME ZONE 'Europe/Malta'
         ELSE NULL END,
    -- created_at: UTC, now()-relative (row birth is not a local wall-clock event).
    CASE WHEN e.status = 'invited' THEN now() - interval '2 days'
         ELSE now() - interval '60 days' END
FROM (VALUES
    -- St Julians (reference crew, home location ...106).
    ('10000000-0000-4000-8000-000000000301'::uuid, '10000000-0000-4000-8000-000000000001'::uuid,
     '10000000-0000-4000-8000-000000000106'::uuid, NULL::uuid,
     'Maria Borg',       'Manager',           'maria.borg@kebabfactory.mt'::citext,       'active'),
    ('10000000-0000-4000-8000-000000000302', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Joseph Camilleri', 'Assistant Manager', 'joseph.camilleri@kebabfactory.mt', 'active'),
    ('10000000-0000-4000-8000-000000000303', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Antoine Vella',    'Chef',              'antoine.vella@kebabfactory.mt',    'active'),
    ('10000000-0000-4000-8000-000000000304', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Rita Zammit',      'Waiter',            'rita.zammit@kebabfactory.mt',      'active'),
    ('10000000-0000-4000-8000-000000000305', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'David Grech',      'Waiter',            'david.grech@kebabfactory.mt',      'active'),
    ('10000000-0000-4000-8000-000000000306', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Sofia Rossi',      'Cashier',           'sofia.rossi@kebabfactory.mt',      'active'),
    ('10000000-0000-4000-8000-000000000307', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Ahmed Hassan',     'Cook',              'ahmed.hassan@kebabfactory.mt',     'active'),
    ('10000000-0000-4000-8000-000000000308', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Elena Petrova',    'Waiter',            'elena.petrova@kebabfactory.mt',    'active'),
    -- Marco Bugeja: physically invited, NULL email (nullable + partial-unique path).
    ('10000000-0000-4000-8000-000000000309', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Marco Bugeja',     'Kitchen Porter',    NULL,                               'active'),
    ('10000000-0000-4000-8000-000000000310', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Nadia Farrugia',   'Waiter',            'nadia.farrugia@kebabfactory.mt',   'active'),
    -- Liam O'Brien: apostrophe doubled for SQL, ASCII only.
    ('10000000-0000-4000-8000-000000000311', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Liam O''Brien',    'Bartender',         'liam.obrien@kebabfactory.mt',      'active'),
    ('10000000-0000-4000-8000-000000000312', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Priya Sharma',     'Cook',              'priya.sharma@kebabfactory.mt',     'active'),
    ('10000000-0000-4000-8000-000000000313', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Carlos Mendez',    'Waiter',            'carlos.mendez@kebabfactory.mt',    'active'),
    -- Grace Attard: invited only -- no activated_at, practice-tap candidate.
    ('10000000-0000-4000-8000-000000000314', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Grace Attard',     'Waiter',            'grace.attard@kebabfactory.mt',     'invited'),
    -- Paul Spiteri: deactivated -- the deactivated-attempt scenario (section 5 row 4).
    ('10000000-0000-4000-8000-000000000315', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', NULL,
     'Paul Spiteri',     'Waiter',            'paul.spiteri@kebabfactory.mt',     'deactivated'),
    -- Rusty Bar night-shift crew (home location ...108).
    ('10000000-0000-4000-8000-000000000316', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000108', NULL,
     'Andrea Micallef',  'Bartender',         'andrea.micallef@kebabfactory.mt',  'active'),
    ('10000000-0000-4000-8000-000000000317', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000108', NULL,
     'Roberta Fenech',   'Bartender',         'roberta.fenech@kebabfactory.mt',   'active'),
    ('10000000-0000-4000-8000-000000000318', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000108', NULL,
     'Ivan Petrov',      'Bar Manager',       'ivan.petrov@kebabfactory.mt',      'active'),
    -- One manager per remaining location.
    ('10000000-0000-4000-8000-000000000319', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000101', NULL,
     'Emma Caruana',     'Manager',           'emma.caruana@kebabfactory.mt',     'active'),
    ('10000000-0000-4000-8000-000000000320', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000102', NULL,
     'Julia Sciberras',  'Manager',           'julia.sciberras@kebabfactory.mt',  'active'),
    ('10000000-0000-4000-8000-000000000321', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000103', NULL,
     'Omar Aziz',        'Manager',           'omar.aziz@kebabfactory.mt',        'active'),
    ('10000000-0000-4000-8000-000000000322', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000104', NULL,
     'George Muscat',    'Manager',           'george.muscat@kebabfactory.mt',    'active'),
    ('10000000-0000-4000-8000-000000000323', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000105', NULL,
     'Kevin Borg',       'Manager',           'kevin.borg@kebabfactory.mt',       'active'),
    ('10000000-0000-4000-8000-000000000324', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000107', NULL,
     'Ryan Gauci',       'Manager',           'ryan.gauci@kebabfactory.mt',       'active'),
    ('10000000-0000-4000-8000-000000000325', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000109', NULL,
     'Stephanie Pace',   'Operations Lead',   'stephanie.pace@kebabfactory.mt',   'active')
) AS e(id, tenant_id, location_id, department_id, full_name, role, email, status)
ON CONFLICT (id) DO NOTHING;

-- ------------------------------------------------- employees (KM, per-department)
-- 11 employees at the single KM facility. Ten belong to a department (their shift
-- comes from the department); Joseph Buttigieg has department_id NULL and so
-- falls back to the KM Plant location shift (09:00-17:00). That NULL-department
-- row is the master data that exercises the section 5 "own shift" resolution split.
-- All active. Same timestamp logic as KF (no invited/deactivated rows here).
INSERT INTO employees
    (id, tenant_id, location_id, department_id, full_name, role, email, status,
     invited_at, activated_at, deactivated_at, created_at)
SELECT
    e.id, e.tenant_id, e.location_id, e.department_id, e.full_name, e.role, e.email, e.status,
    now() - interval '60 days',
    (((now() AT TIME ZONE 'Europe/Malta')::date - 58) + TIME '09:30')
        AT TIME ZONE 'Europe/Malta',
    NULL,
    now() - interval '60 days'
FROM (VALUES
    ('20000000-0000-4000-8000-000000000301'::uuid, '20000000-0000-4000-8000-000000000001'::uuid,
     '20000000-0000-4000-8000-000000000101'::uuid, '20000000-0000-4000-8000-000000000201'::uuid,
     'Antonia Grima',    'Plant Manager',    'antonia.grima@kebabmfg.mt'::citext, 'active'),
    ('20000000-0000-4000-8000-000000000302', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000201',
     'Frank Debono',     'Admin',            'frank.debono@kebabmfg.mt',    'active'),
    ('20000000-0000-4000-8000-000000000303', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000202',
     'Saviour Bonnici',  'Butcher',          'saviour.bonnici@kebabmfg.mt', 'active'),
    ('20000000-0000-4000-8000-000000000304', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000202',
     'Dragan Nikolic',   'Meat Operative',   'dragan.nikolic@kebabmfg.mt',  'active'),
    ('20000000-0000-4000-8000-000000000305', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000203',
     'Charlene Vella',   'Warehouse Lead',   'charlene.vella@kebabmfg.mt',  'active'),
    ('20000000-0000-4000-8000-000000000306', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000203',
     'Peter Falzon',     'Forklift Operator','peter.falzon@kebabmfg.mt',    'active'),
    ('20000000-0000-4000-8000-000000000307', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000204',
     'Rosaria Schembri', 'Line Cook',        'rosaria.schembri@kebabmfg.mt','active'),
    ('20000000-0000-4000-8000-000000000308', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000204',
     'Yusuf Demir',      'Cook',             'yusuf.demir@kebabmfg.mt',     'active'),
    ('20000000-0000-4000-8000-000000000309', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000205',
     'Doris Micallef',   'Baker',            'doris.micallef@kebabmfg.mt',  'active'),
    ('20000000-0000-4000-8000-000000000310', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', '20000000-0000-4000-8000-000000000205',
     'Ana Silva',        'Pastry Chef',      'ana.silva@kebabmfg.mt',       'active'),
    -- Joseph Buttigieg: NO department -> falls back to KM Plant location shift.
    ('20000000-0000-4000-8000-000000000311', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', NULL,
     'Joseph Buttigieg', 'Maintenance',      'joseph.buttigieg@kebabmfg.mt','active')
) AS e(id, tenant_id, location_id, department_id, full_name, role, email, status)
ON CONFLICT (id) DO NOTHING;

-- ------------------------------------------------------------------------ tags
-- Wall-mounted NFC plaques (NTAG 424 DNA). uid = 14-hex UID (char(14) in the DB).
-- One active plaque per KF location + one at the KM facility. Plus two lifecycle
-- fixtures: a RETIRED St Julians plaque whose replaced_by points to the current
-- St Julians plaque (the "Replace tag" audit chain, and a retired-tap reject
-- fixture), and a LOST Hamrun plaque (a lost-tap reject fixture). The self-FK
-- (replaced_by, tenant_id) -> tags(uid, tenant_id) is checked at statement end,
-- so the referenced active plaque may sit in the same INSERT.
--
-- aes_key_ref below is only a PLACEHOLDER and is NOT usable: the real value is a
-- 44-byte KEK-wrapped envelope written by the second half of scripts/seed.sh (see
-- SECRETS above). The text says so in the value itself, so a byte dump taken
-- between the two halves explains its own state instead of looking like a corrupt
-- key. The label is exactly 30 characters so that label || uid is exactly 44
-- bytes, which is what migration 00021's tags_aes_key_ref_is_kek_envelope demands
-- of every row -- see the SECRETS note above before touching it.
--
-- 🔴 THE LABEL BELOW IS ALSO DECLARED IN GO, as fixtures.SeedPlaceholderKeyLabel,
-- and seedkeys' drift guard compares tags.aes_key_ref against it BYTE FOR BYTE --
-- because since the padding to 44 bytes a length check can no longer tell a
-- forgotten plaque from a wrapped one (measured; the guard's own comment carries
-- the probe). This file is loaded by psql and cannot import a Go constant, so
-- CHANGING THE TEXT HERE MEANS CHANGING IT THERE. Forgetting is survivable but
-- not free: the guard's third predicate still catches any all-printable value,
-- so the plaque is still refused, only with a vaguer reason.
-- last_ctr keeps its DEFAULT 0 (untapped in master data; taps advance it
-- atomically). retired_at is stamped only on the retired plaque.
INSERT INTO tags
    (uid, tenant_id, location_id, aes_key_ref, status, retired_at, replaced_by, created_at)
SELECT
    t.uid, t.tenant_id, t.location_id,
    convert_to('NOT-A-KEY-seedkeys-REWRITES-IT' || t.uid, 'UTF8'),
    t.status,
    CASE WHEN t.status = 'retired' THEN now() - interval '20 days' ELSE NULL END,
    t.replaced_by,
    now() - interval '80 days'
FROM (VALUES
    ('04AC7E55000101'::char(14), '10000000-0000-4000-8000-000000000001'::uuid,
     '10000000-0000-4000-8000-000000000101'::uuid, 'active',  NULL::char(14)),
    -- Lost Hamrun plaque -- lost-tap reject fixture (section 5 row 1).
    ('04AC7E550001FF', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000101', 'lost',    NULL),
    ('04AC7E55000201', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000102', 'active',  NULL),
    ('04AC7E55000301', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000103', 'active',  NULL),
    ('04AC7E55000401', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000104', 'active',  NULL),
    ('04AC7E55000501', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000105', 'active',  NULL),
    -- Current St Julians plaque (referenced by the retired one below).
    ('04AC7E55000601', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', 'active',  NULL),
    -- Retired St Julians plaque -- replaced_by the current one (audit chain +
    -- retired-tap reject fixture, section 5 row 1).
    ('04AC7E550006AA', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000106', 'retired', '04AC7E55000601'),
    ('04AC7E55000701', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000107', 'active',  NULL),
    ('04AC7E55000801', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000108', 'active',  NULL),
    ('04AC7E55000901', '10000000-0000-4000-8000-000000000001',
     '10000000-0000-4000-8000-000000000109', 'active',  NULL),
    -- KM facility plaque.
    ('04BE7E55000A01', '20000000-0000-4000-8000-000000000001',
     '20000000-0000-4000-8000-000000000101', 'active',  NULL)
) AS t(uid, tenant_id, location_id, status, replaced_by)
ON CONFLICT (uid) DO NOTHING;

-- ------------------------------------------------------------------ admin_users
-- ONE panel owner per tenant (M1-11). role='owner', status='active'. This is the
-- panel identity -- SEPARATE from employees (an employee never has a password).
-- Idempotent via fixed UUID (...4xx admin block) + ON CONFLICT DO NOTHING.
--
-- DEV-ONLY PASSWORD -- NOT A REAL SECRET (section 4.7). password_hash is a bcrypt
-- $2a$ hash (cost 12) of the DOCUMENTED dev-only password "tappa-dev-only-changeme"
-- (mirrored as fixtures.AdminDevPassword in ids.go). Generated OFFLINE with Go's
-- golang.org/x/crypto/bcrypt; x/crypto is NOT a project dependency -- the hash is a
-- static string embedded here (same principle as the fake tag keys above). Each
-- owner has its own salt (distinct hash), as bcrypt normally produces. Real owners
-- (M8-07) set their own password; this credential is for the demo panel only.
INSERT INTO admin_users
    (id, tenant_id, full_name, email, password_hash, role, status, created_at)
VALUES
    ('10000000-0000-4000-8000-000000000401', '10000000-0000-4000-8000-000000000001',
     'KF Owner', 'owner@kebabfactory.mt'::citext,
     -- DEV ONLY -- not a real secret; bcrypt($2a$) of "tappa-dev-only-changeme".
     '$2a$12$XqFC1yKNh9FGZ4bIx26ZWuchg1hmT1c4p7zWpzyxFeIQMOBmoRTji',
     'owner', 'active', now() - interval '90 days'),
    ('20000000-0000-4000-8000-000000000401', '20000000-0000-4000-8000-000000000001',
     'KM Owner', 'owner@kebabmfg.mt'::citext,
     -- DEV ONLY -- not a real secret; bcrypt($2a$) of "tappa-dev-only-changeme".
     '$2a$12$dJ.K9p.uwM2Inl5J/cMcPOd5w6VLagLkjq41XYGu/TCfo3d.SDRCC',
     'owner', 'active', now() - interval '90 days')
ON CONFLICT (id) DO NOTHING;

COMMIT;
