-- 00087 -- T187's CONTRACT (ADR 0036, Karar 3): tappa_app loses its INSERT on admin_sessions. A panel
-- session is born only inside a SECURITY DEFINER function of ADR 0031 §5.2's closed list --
-- identity_open_row_session (00086, the single admin_users row), identity_open_session and
-- identity_switch_session (00071/00085, the identity) -- all three owned by tappa_identitydefiner,
-- which holds its own INSERT (tenant_id, admin_user_id, token_hash, authenticated_at) (00071).
--
-- WHY. After 00086 the password proof, the status, the tenant and unlinked_at are checked in the
-- database -- but only for the sessions born in the function. As long as tappa_app could send its own
-- `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES (...)`, a process running
-- arbitrary SQL as tappa_app bypassed all of it: a session for a refused membership (unlinked_at, which
-- tappa_app can neither read nor write -- R16), with no audit row (ADR 0036, "Kabul edilen kalan risk",
-- threat 1). This file is what makes those guarantees hold.
--
-- MEASURED BEFORE WRITING (goose 86):
--   * the grant: tappa_app holds INSERT (id, tenant_id, admin_user_id, token_hash) on admin_sessions,
--     column-level only (00071:574-575; \dp, information_schema.column_privileges), no table-level INSERT;
--   * no function inserts into admin_sessions with tappa_app's rights: the three pg_proc bodies that do
--     are the definers above (prosecdef, owner tappa_identitydefiner); the one trigger on the table
--     (admin_sessions_revocation_monotonic) writes nothing; tappa_app is a member of no role;
--   * the running binary (main 232f06f8, Deploy 6) sends no INSERT into admin_sessions: its two births
--     call db.OpenRowSession, the generated CreateAdminSession has no caller
--     (internal/adminauth/rowsession_test.go read that tree). The migrate Job runs before the rollout
--     while that pod still serves (replicas 1, maxSurge 1, maxUnavailable 0): it keeps signing people
--     in through the definer. The binary before 00086 runs nowhere (ADR 0031 §8: expand, then contract
--     in the NEXT deploy).
--
-- THE FORM. A table-level REVOKE also clears the column-level grants (PostgreSQL: "revoking a table
-- privilege also revokes the column privileges"; 00071's Down relies on it): measured, after it
-- has_column_privilege(tappa_app, admin_sessions, <col>, INSERT) is false for all eight columns.
-- tappa_app keeps SELECT (00006) and UPDATE (last_used_at, revoked_at) (00011) -- the touch and the
-- sign-out; it never had DELETE.
--
-- LOCKS. A REVOKE takes no lock on admin_sessions (measured: pg_locks shows none for the relation, and
-- it returns at once while another transaction holds ROW EXCLUSIVE on the table -- 00085's measurement
-- for a column GRANT). The lock_timeout is 00071's and 00085's hot-table precedent, kept so a stuck
-- catalogue row fails this file fast instead of queueing the sign-ins behind it.
--
-- THE DOWN gives back 00071's grant byte for byte: INSERT (id, tenant_id, admin_user_id, token_hash).

-- +goose Up
SET LOCAL lock_timeout = '1s';
REVOKE INSERT ON admin_sessions FROM tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF has_any_column_privilege('tappa_app', 'public.admin_sessions', 'INSERT') THEN
        RAISE EXCEPTION '00087: tappa_app still holds an INSERT on admin_sessions';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- A rollback of the deploy: the binary this file ships with sends no INSERT either, so the grant
-- comes back only for the shape of the catalogue (00071's), not because a caller needs it.
SET LOCAL lock_timeout = '1s';
GRANT INSERT (id, tenant_id, admin_user_id, token_hash) ON admin_sessions TO tappa_app;
