-- 00116 -- the owner's "these are my staff's real shifts" switch (M16-45, ADR 0050 (iv); Q52 (2) (c),
-- the user's answer of 2026-11-19).
--
-- A venue's shift is often its OPENING WINDOW (the seed's KF venue: 10:00-22:00), not a person's
-- shift. Shown to a person as "Usual shift", or used as the moment a "Still at work?" reminder
-- fires, an opening window would be a false statement. Only the owner knows which of the two the
-- stored values are, so the staff app uses a shift ONLY when this switch is on: the usual-shift
-- line, the reminder's shift arm and the lock-screen "Shift ends" line. Off (the default), the
-- app's reminder still works -- inSince + 10 h, clipped to the business day (Q51 (2) (b), (d)).
--
-- The switch changes nothing the engine decides: late minutes, the claim prefill (K3) and the
-- manager's approval band already use any shift that is set (ADR 0032 §C.4), with or without it.
--
-- tenants hands tappa_app SELECT table-wide and UPDATE column by column (00024, 00064): this column
-- is added to the UPDATE list and to nothing else -- the panel's owner sets it, in the same
-- transaction as its audit row (tenant.staff_shift_updated). No INSERT: a new tenant starts off.
-- PG11+ stores a constant DEFAULT in the catalogue, so the ALTER rewrites no row.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE tenants
    ADD COLUMN staff_shift_visible boolean NOT NULL DEFAULT false;

GRANT UPDATE (staff_shift_visible) ON tenants TO tappa_app;

-- +goose Down
-- Configuration, not evidence: the switch's history is audit_log's (tenant.staff_shift_updated).
SET LOCAL lock_timeout = '1s';

REVOKE UPDATE (staff_shift_visible) ON tenants FROM tappa_app;

ALTER TABLE tenants
    DROP COLUMN staff_shift_visible;
