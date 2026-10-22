-- +goose NO TRANSACTION
-- 00070 -- M14-03: UNIQUE (tenant_id, identity_id) on admin_users -- one membership per identity per
-- business (ADR 0031 §5.1, bulgu 19). It is what M14-05's identity_relink_refused leans on ("another
-- membership of the identity in that tenant -> 0 rows") and what keeps a switch's target membership
-- one row. identity_id NULL (an unlinked row, every row today) is distinct from every other NULL, so
-- a business keeps any number of unlinked rows.
--
-- A FILE OF ITS OWN, CONCURRENTLY, OUTSIDE A TRANSACTION (ADR 0031 §5.1; CLAUDE.md §6's hot-table
-- rule): admin_users is read on every panel request and written on every login, and a plain CREATE
-- INDEX holds SHARE on it for the build -- every login's UPDATE of last_login_at would queue behind
-- it. CONCURRENTLY takes SHARE UPDATE EXCLUSIVE, which no DML waits for; what it waits for itself is
-- the transactions older than its snapshots (a long reader -- the 02:30 dump -- holds it, never the
-- panel). No lock_timeout here: the build blocks no request, and in a NO TRANSACTION file a session
-- setting would outlive the statement on goose's pooled connection.
--
-- RE-RUNNABLE: an interrupted CONCURRENTLY build leaves an INVALID index behind, and goose records a
-- NO TRANSACTION file only at its end -- so the Up first drops a leftover of that name (only this
-- file ever makes it) and builds it again. The Down drops it the same way.

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS public.admin_users_tenant_identity_key;
CREATE UNIQUE INDEX CONCURRENTLY admin_users_tenant_identity_key ON public.admin_users (tenant_id, identity_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.admin_users_tenant_identity_key;
