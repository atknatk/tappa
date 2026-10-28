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
--
-- THE DOWN REFUSES WHILE AN IDENTITY EXISTS -- 00069's Down guard, word for word (M14-03 audit,
-- measured): `goose down-to 68` runs THIS Down first, and without the guard it would drop the index
-- and only then meet 00069's refusal, leaving a database at version 69 with identities and links and
-- no one-membership-per-identity-per-business index. The guard is a statement of its own and runs
-- before the DROP (a DO block runs fine outside a transaction); an identity committed between the
-- two is the window 00069's own Down closes with its table lock, and that Down refuses then.

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS public.admin_users_tenant_identity_key;
CREATE UNIQUE INDEX CONCURRENTLY admin_users_tenant_identity_key ON public.admin_users (tenant_id, identity_id);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identities)
       OR EXISTS (SELECT 1 FROM public.admin_users WHERE identity_id IS NOT NULL OR unlinked_at IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00070 Down: admin_identities holds identities (or a membership row is linked or refused); the one-membership-per-identity index stays while they do, and 00069 Down refuses them too. Go back with the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd
DROP INDEX CONCURRENTLY IF EXISTS public.admin_users_tenant_identity_key;
