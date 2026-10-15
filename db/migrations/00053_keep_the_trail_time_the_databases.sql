-- 00053 -- M11 API-6 (review, both lenses): the application cannot write audit_log.at.
--
-- WHY NOW. audit_log.at was a record of WHEN an act happened; since API-6 it also carries a
-- security decision: K-25's deactivation budget (ADR 0028 §12) counts a key's deactivations of
-- the last hour from `at`, and the owner's alert reads it the same way. 00005 granted tappa_app
-- table-level INSERT, so an INSERT that names `at` was accepted with any value (measured by both
-- review lenses): a row dated an hour back leaves the budget, a row dated ahead stays in it.
-- The product never names the column -- the one INSERT tappa_app runs is
-- db/queries/audit.sql's RecordAuditEvent, which leaves `at` to its DEFAULT now() -- so the grant
-- is narrowed to the columns that statement writes, plus `id` (it has a DEFAULT too; tests name it
-- and a chosen id is no fact about time).
--
-- WHAT IT DOES NOT TOUCH. The definers that write a tenant's trail with an explicit `at`
-- (clock_timestamp(), migrations 00034, 00039 and 00042) do so under their own role's column
-- grant, which this file neither reads nor changes. SELECT stays table-level; UPDATE and DELETE stay
-- revoked (00005) and the append-only trigger stays.
--
-- ⚠️ A COLUMN GRANT IS NOT A TABLE GRANT: has_table_privilege(tappa_app, audit_log, INSERT) is
-- false from here on, has_column_privilege per column says what is writable. internal/audit's
-- TestAuditLog_IsAppendOnlyForTheApp reads both.

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_table_privilege('tappa_app', 'public.audit_log', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00053 narrows tappa_app''s table-level INSERT on audit_log, and tappa_app has none. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

REVOKE INSERT ON public.audit_log FROM tappa_app;
GRANT INSERT (id, tenant_id, actor_id, action, target, detail) ON public.audit_log TO tappa_app;

-- The postcondition reads EFFECTIVE privilege (a membership or PUBLIC would answer true too).
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.audit_log', 'at', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00053: tappa_app can still write audit_log.at (a membership or PUBLIC grants it). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT pg_catalog.has_column_privilege('tappa_app', 'public.audit_log', 'detail', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00053: tappa_app cannot write audit_log.detail after the narrowing. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

REVOKE INSERT (id, tenant_id, actor_id, action, target, detail) ON public.audit_log FROM tappa_app;
GRANT INSERT ON public.audit_log TO tappa_app;
