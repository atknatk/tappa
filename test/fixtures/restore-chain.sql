-- Tappa -- the restore chain's fixture (T215; scripts/restore-chain-check.sh, `make restore-check`).
--
-- WHAT THIS IS FOR. scripts/pg-restore-verify.sh compares a restored database with its dump, and a
-- comparison of empty tables proves little: section 6 (the punch feed's counters) only has something
-- to judge where a tenant has a counter that moved, and the row counts only bite where there are rows.
-- test/fixtures/seed.sql is master data and writes none of the append-only tables (its header says
-- why), so this file puts at least one row into each of the twelve append-only tables
-- pg-restore-verify.sh's trunc_tables names, and moves one tenant's feed counter, the way the product
-- writes them: transactions through the punch_feed trigger (punch_events and feed_counters are never
-- written here by hand), a review through its validation trigger, a claim and its decision through
-- theirs. The site projection is the one row written directly: holding_feed would need a holding,
-- and the table is append-only either way.
--
-- NOT DEMO DATA. It is loaded only into the chain's own throwaway source container, never into the
-- development or CI database (it would change the counts the seeded tests read). Every value is
-- fixed and fake: the tag's aes_key_ref is 44 bytes of ASCII that says it is not a key (CLAUDE.md
-- §4.7), the admin's password hash is bcrypt's shape and the hash of nothing, and no row carries an
-- e-mail address. Loaded once into an empty database; a second load fails on the first primary key, loudly.
--
-- EVERY COLUMN A CHECK OR A TRIGGER READS IS NAMED, never left to its default: a default is a
-- migration's to change (m14-dbprivs' 00090 turns tags.status's to 'unassigned', and a plaque with a
-- location is then refused by tags_unassigned_has_no_location -- in review, at v90). So tags.status,
-- admin_users.status, policies.enabled and transactions.practice (punch_feed and the claim trigger
-- read it) are written out; the defaults left are ids, timestamps and display values nothing checks.
--
-- Run as tappa_owner, in the tenant's own context (SET LOCAL, CLAUDE.md §6): punch_feed refuses a
-- row whose tenant is not app.tenant_id, and one transaction per tenant because a transaction's feed
-- events may use only the counter set its first event locked (ADR 0028 §5).

BEGIN;
SET LOCAL app.tenant_id = 'c4a10000-0000-4000-8000-000000000001';

-- The employer. tenants_feed_counter (AFTER INSERT) opens its feed counter at 0.
INSERT INTO tenants (id, name, vat_number, business_type, structure)
VALUES ('c4a10000-0000-4000-8000-000000000001', 'Restore Chain Employer', 'MT00000001', 'restaurant', 'single');

INSERT INTO locations (id, tenant_id, name)
VALUES ('c4a10000-0000-4000-8000-000000000011', 'c4a10000-0000-4000-8000-000000000001', 'Restore Chain Kitchen');

INSERT INTO departments (id, tenant_id, location_id, name)
VALUES ('c4a10000-0000-4000-8000-000000000021', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000011', 'Restore Chain Line');

INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status, activated_at)
VALUES ('c4a10000-0000-4000-8000-000000000031', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000011', 'c4a10000-0000-4000-8000-000000000021',
        'Restore Chain Cook', 'active', now() - interval '30 days');

INSERT INTO admin_users (id, tenant_id, full_name, password_hash, role, status)
VALUES ('c4a10000-0000-4000-8000-000000000041', 'c4a10000-0000-4000-8000-000000000001', 'Restore Chain Owner',
        -- bcrypt's SHAPE (admin_users_password_hash_is_bcrypt) and no hash of anything: no
        -- password opens it, and nothing logs in to this database.
        '$2a$12$' || repeat('x', 53), 'owner', 'active');

INSERT INTO tags (uid, tenant_id, location_id, status, aes_key_ref)
VALUES ('C4A1000000000A', 'c4a10000-0000-4000-8000-000000000001', 'c4a10000-0000-4000-8000-000000000011', 'active',
        convert_to('NOT-A-KEY-restore-chain-fixture-only-T215...', 'UTF8'));

INSERT INTO policies (id, tenant_id, name, layer, enabled)
VALUES ('c4a10000-0000-4000-8000-000000000051', 'c4a10000-0000-4000-8000-000000000001', 'restore-chain', 'tenant', true);

INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document)
VALUES ('c4a10000-0000-4000-8000-000000000052', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000051', 1, '{}');

-- An ok check-in on the plaque: punch_feed writes punch_events seq 1 and moves the counter to 1.
INSERT INTO transactions (id, tenant_id, employee_id, location_id, tag_uid, ctr, type, occurred_at, verdict,
                          channel, sun_valid, trust, business_day_start, practice)
VALUES ('c4a10000-0000-4000-8000-000000000061', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000031', 'c4a10000-0000-4000-8000-000000000011', 'C4A1000000000A',
        1, 'in', now() - interval '3 hours', 'ok', 'nfc', true, 20, date_trunc('day', now() - interval '3 hours'), false);

-- A flagged tap and the manager's approval: the review's punch_feed writes seq 2.
INSERT INTO transactions (id, tenant_id, employee_id, location_id, tag_uid, ctr, type, occurred_at, verdict,
                          channel, sun_valid, trust, business_day_start, practice)
VALUES ('c4a10000-0000-4000-8000-000000000062', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000031', 'c4a10000-0000-4000-8000-000000000011', 'C4A1000000000A',
        2, 'out', now() - interval '2 hours', 'flag', 'nfc', true, 20, date_trunc('day', now() - interval '3 hours'), false);

INSERT INTO transaction_reviews (id, tenant_id, transaction_id, reviewer_id, outcome)
VALUES ('c4a10000-0000-4000-8000-000000000071', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000062', 'c4a10000-0000-4000-8000-000000000041', 'approved');

-- A claim of the missing half after the open check-in, and its rejection.
INSERT INTO punch_claims (id, tenant_id, employee_id, anchor_transaction_id, kind, claimed_at)
VALUES ('c4a10000-0000-4000-8000-000000000081', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000031', 'c4a10000-0000-4000-8000-000000000061', 'out',
        now() - interval '150 minutes');

INSERT INTO punch_claim_decisions (id, tenant_id, claim_id, outcome, decided_by)
VALUES ('c4a10000-0000-4000-8000-000000000082', 'c4a10000-0000-4000-8000-000000000001',
        'c4a10000-0000-4000-8000-000000000081', 'rejected', 'c4a10000-0000-4000-8000-000000000041');

INSERT INTO audit_log (tenant_id, actor_id, action, target)
VALUES ('c4a10000-0000-4000-8000-000000000001', 'c4a10000-0000-4000-8000-000000000041',
        'restore_chain.fixture', 'c4a10000-0000-4000-8000-000000000062');

INSERT INTO billing_periods (tenant_id, period_month, period_from, period_to, timezone, plan, free_period,
                             employee_count, unit_price, closed_by)
VALUES ('c4a10000-0000-4000-8000-000000000001', date '2026-10-01', timestamptz '2026-09-30 22:00:00+00',
        timestamptz '2026-10-31 23:00:00+00', 'Europe/Malta', 'founding', true, 1, 1.50,
        'c4a10000-0000-4000-8000-000000000041');

-- The plan history (00097, T27), the way the product writes it: a plan change on tenants, and the
-- tenants_plan_history trigger writes the tenant's first row (its founding plan, since signup) and
-- the change (from the wall clock -- after October, which stays founding).
UPDATE tenants SET plan = 'standard' WHERE id = 'c4a10000-0000-4000-8000-000000000001';
COMMIT;

BEGIN;
SET LOCAL app.tenant_id = 'c4a10000-0000-4000-8000-000000000002';

-- The site: another tenant, with the employer's check-in projected onto it.
INSERT INTO tenants (id, name, vat_number, business_type, structure)
VALUES ('c4a10000-0000-4000-8000-000000000002', 'Restore Chain Site', 'MT00000002', 'hotel', 'single');

INSERT INTO site_punches (tenant_id, transaction_id, employer_tenant_id, employee_id, bucket_hour, occurred_at,
                          channel, type, outcome)
VALUES ('c4a10000-0000-4000-8000-000000000002', 'c4a10000-0000-4000-8000-000000000061',
        'c4a10000-0000-4000-8000-000000000001', 'c4a10000-0000-4000-8000-000000000031',
        date_trunc('hour', now(), 'UTC'), now() - interval '3 hours', 'nfc', 'in', 'counted');
COMMIT;

-- The tenant-unscoped two (ADR 0020/0021): no tenant context.
INSERT INTO legal_documents (slug, body) VALUES ('privacy', 'Restore chain fixture: not a published document.');
INSERT INTO operator_audit_log (kind) VALUES ('login_failed');
