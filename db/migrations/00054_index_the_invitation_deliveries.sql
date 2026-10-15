-- 00054 -- M11 API-6 (review, third round, D-3): an index for the activation e-mail's state.
--
-- WHAT READS IT. GET /v1/employees (ListAPIEmployees) and GET /v1/employees/{ref} (GetAPIEmployee)
-- answer each employee's activationEmail from the trail: the person's LAST delivery attempt
-- (invite.code_emailed, invite.code_shown_to_manager, invite.undelivered, invite.email_refused) and
-- whether a live invitation has its delivery row. Those four actions are a small part of a
-- business's trail; audit_log_tenant_target_idx (00021) finds a person's rows but then reads every
-- one of them, of every action.
--
-- MEASURED (a disposable PostgreSQL 17 seeded for it -- one business, two thousand people, 240 000
-- trail rows -- a page of 500 read per row with LATERAL): without this index 75-125 ms, with it 6.3-6.8 ms (1 000 rows: 13 ms); the
-- single read 3.6 ms -> 0.2 ms. The figures are in ADR 0028's API-6 note.
--
-- PARTIAL, so it holds the four actions alone and costs nothing on the rest of the trail's writes.
-- tenant_id leads (CLAUDE.md §6's index shape). Built inside goose's transaction (not CONCURRENTLY):
-- it takes a SHARE lock on audit_log for the build, so trail writes wait for it -- the table is a
-- pilot's today; API-13's runbook measures its size before the deploy.
--
-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT (API-6 review D-b, measured: behind an open audit_log
-- transaction the build waited 5.3 s and the next trail write queued behind it failed at 3 s; and
-- op_remove_holding_member holds the membership row while it waits for audit_log -- the holding tap's
-- loss class, ADR 0027 §13 item 16). With it the migration is the side that gives up: the migrate Job
-- fails, nothing is rolled out, and the file is run again at a quiet moment (deploy/README.md, the
-- lock_timeout rule; the size past which this needs CONCURRENTLY in a migration of its own is there).

-- +goose Up
SET LOCAL lock_timeout = '1s';
CREATE INDEX audit_log_invite_delivery_idx
    ON public.audit_log (tenant_id, target, at)
    WHERE action IN ('invite.code_emailed', 'invite.code_shown_to_manager', 'invite.undelivered', 'invite.email_refused');

-- +goose Down
SET LOCAL lock_timeout = '1s';
DROP INDEX public.audit_log_invite_delivery_idx;
