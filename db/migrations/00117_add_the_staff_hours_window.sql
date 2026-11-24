-- 00117 -- the owner's "My hours" setting (M16-30; Q48 (a), the user's answer of 2026-11-19).
--
-- My hours lets a person read their own records and weekly totals in the staff app. The BUSINESS
-- manages it (the user's decision 4, 2026-11-18): the owner turns it on and picks how far back it
-- reaches, and the server enforces both on every request (internal/handler/apphours.go).
--
-- The value is a number of WHOLE WEEKS: "N weeks" is this week (Monday to today) and the N-1 weeks
-- before it, in the business's zone (ledger.WeekOf). A window counted in days would cut a week and
-- show a short week's total as complete; whole weeks are the panel report's own unit.
--
-- NULL is OFF, and every row starts NULL (Q48 (a): no business sees it turned on for it). No
-- DEFAULT, so the ALTER rewrites nothing.
--
-- It is not a policy (internal/policy returns effects on a tap's decision): it changes no verdict,
-- no trust and no direction -- it gates a read surface, like the business day cutoff (00064).
-- tenants hands tappa_app UPDATE column by column (00024, 00064, 00116): this column joins the list.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE tenants
    ADD COLUMN staff_hours_weeks smallint,
    ADD CONSTRAINT tenants_staff_hours_weeks_is_an_option
        CHECK (staff_hours_weeks IS NULL OR staff_hours_weeks IN (1, 2, 4, 13));

GRANT UPDATE (staff_hours_weeks) ON tenants TO tappa_app;

-- +goose Down
-- Configuration, not evidence: the setting's history is audit_log's (tenant.staff_hours_updated).
SET LOCAL lock_timeout = '1s';

REVOKE UPDATE (staff_hours_weeks) ON tenants FROM tappa_app;

ALTER TABLE tenants
    DROP CONSTRAINT tenants_staff_hours_weeks_is_an_option,
    DROP COLUMN staff_hours_weeks;
