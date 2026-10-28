-- 00083 -- M14-19: the migration of the deploy that turns TAPPA_ADMIN_IDENTITY on (ADR 0031 §8 step 5,
-- §5.8; the plan's T9, S5, S18). Two jobs and nothing else, both deterministic, both tested
-- (internal/db/adminflag_test.go) -- never a hand UPDATE:
--
--   1. THE CONFIRMATION WINDOWS (S5). Every window opened while the flag was off gets its END moved to
--      this file's instant + 7 days. ONLY confirm_open_until is written: confirm_opened_at stays,
--      because the reset scope's list IS `linked_at < confirm_opened_at` (00072, 00079) and the rows
--      refused inside the window are `unlinked_at >= confirm_opened_at` -- moving the opening would
--      change WHICH memberships the person is asked about. confirm_scope stays. The result is the
--      ADR's window: max(the event, the flag's day) + 7 days.
--   2. identity_create_for_signup LOSES tappa_app's EXECUTE (S18). M14-17 (00078) stopped the binary
--      calling it; the binary before this one (main 8d94a95e) and this one carry no caller
--      (TestIdentityCreateForSignup_NoBinaryCallsIt), so the old pod of this deploy's rollout cannot
--      reach it either -- the expand/contract rule (ADR 0031 §8) holds. The function itself STAYS:
--      its DROP is contract B's (M14-20b), with resolve_admin_by_email and identity_reconcile.
--
-- WHICH WINDOWS: `confirm_opened_at IS NOT NULL`, every one. Nothing in the schema records the flag:
-- it is an environment value of the pod (deploy/k8s/05-config.yaml). What the schema DOES record is
-- that a window exists (00069's admin_identities_confirm_window_whole: opened_at, open_until and
-- scope are NULL together, so opened_at IS NOT NULL is "a window exists"), and the flag has been OFF
-- on every database up to this file -- no deploy before this one shipped it on. While it is off,
-- nothing can CLOSE a window but its own clock: the list, "Keep all" (identity_keep_all NULLs the
-- three columns), the unlink and the undo are account surfaces the flag holds shut (ADR 0031 §8's
-- table). So every window this file sees was opened while the flag was off, and an EXPIRED one is
-- included on purpose: its person never saw the list, the ADR's max(event, flag day) + 7 reopens it.
-- A window closed by "Keep all" has no columns and is not touched.
--
-- 🔴 THAT READING HOLDS ONLY IF THIS FILE RUNS IN THE DEPLOY THAT TURNS THE FLAG ON. The migrate Job
-- runs before the rollout, so "now" here is the flag's day. Two named gaps (deploy/README.md, "M14-19
-- -- bayrak"): (a) the old pod of the same rollout (flag off) can open a window in the minutes between
-- this Job and its replacement; that window ends at its own event + 7 days, minutes short of the
-- flag's day + 7 -- accepted. (b) Shipped in an EARLIER deploy than the flag commit, every window
-- opened between the two deploys keeps its event + 7 days and can close before the flag shows it --
-- the runbook says to ship the two together; splitting them is the user's decision, with that cost.
-- A later off -> on cycle (a rollback and a second turn) does not run this file again either.
--
-- GREATEST, not a bare now() + 7: a window opened by a transaction that committed after this one
-- began (READ COMMITTED: the UPDATE sees it) already ends after now() + 7 days by seconds; GREATEST
-- keeps its own end instead of shortening it. For every other window the two are equal.
-- 00069's admin_identities_confirm_window_forward (open_until > opened_at) holds: opened_at <= the
-- end it already had <= the new end.
--
-- WHO WRITES IT: tappa_owner, the migration role -- the third named maintenance writer of CLAUDE.md
-- §4.5 (3) / ADR 0031 §6.1 ("M14-19'un migration'ı ... yalnız bayrak kapalıyken açılmış onay
-- pencerelerinin confirm_open_until'i"). admin_identities is FORCE ROW LEVEL SECURITY with no policy
-- for any role: a migration role that is neither superuser nor BYPASSRLS would see 0 rows and
-- "move" nothing, silently -- the precondition refuses that database (00073's / 00082's assertion).
--
-- LOCKS: the UPDATE row-locks the identities that have a window (a handful; admin_identities has no
-- trigger); a live writer holding one (a sign-in's last_login_at, a reset's consumption) is waited for
-- under this file's lock_timeout, and past it the file fails with 55P03 and writes NOTHING (one
-- transaction) -- the Job is `Failed`, run it again at a quiet moment. The REVOKE rewrites one
-- pg_proc row. No table lock.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = current_user AND (rolsuper OR rolbypassrls)) THEN
        RAISE EXCEPTION 'migration 00083 (M14-19): the running role is neither superuser nor BYPASSRLS; under FORCE ROW LEVEL SECURITY it would see no confirmation window and move none (ADR 0031 5.8). Run the migrations as tappa_owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regprocedure('public.identity_create_for_signup(uuid,uuid)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_keep_all(text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_reconcile()') IS NULL THEN
        RAISE EXCEPTION 'migration 00083 (M14-19) needs 00074 (identity_create_for_signup), 00079 (the confirmation window) and 00082 (the reconciliation). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The confirmation windows: the end alone, to the flag's day + 7 days
-- ---------------------------------------------------------------------------
UPDATE public.admin_identities
   SET confirm_open_until = GREATEST(confirm_open_until, now() + interval '7 days')
 WHERE confirm_opened_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2. identity_create_for_signup: no caller left, so no EXECUTE for tappa_app
-- ---------------------------------------------------------------------------
REVOKE EXECUTE ON FUNCTION public.identity_create_for_signup(uuid, uuid) FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 3. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f oid := 'public.identity_create_for_signup(uuid,uuid)'::pg_catalog.regprocedure;
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identities
                WHERE confirm_opened_at IS NOT NULL
                  AND confirm_open_until < now() + interval '7 days') THEN
        RAISE EXCEPTION 'migration 00083 postcondition: a confirmation window still ends before the flag''s day + 7 days (ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The door is shut to every role but its owner, and the function is otherwise as 00076 left it:
    -- still there, still the definer's, still pinned to its search path (DROP is contract B's).
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                    WHERE p.oid = f AND p.prosecdef
                      AND p.proconfig = ARRAY['search_path=pg_catalog, pg_temp']
                      AND p.proowner = 'tappa_identitydefiner'::pg_catalog.regrole)
       OR pg_catalog.has_function_privilege('tappa_app', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_operator', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_opdefiner', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_resolver', f, 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00083 postcondition: identity_create_for_signup is still callable by a role other than its owner, or is not the shape 00076 left. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The grant comes back exactly as 00076 left it. The WINDOWS ARE LEFT AS THEY ARE, on purpose: the
-- ends they had before the Up were not kept anywhere (a column for them would be schema for a
-- one-off), and a longer window is harmless where a Down is taken -- with the flag off nothing shows,
-- closes or acts on a window (ADR 0031 §8's table); with it on, a shortened window would take a list
-- away from a person mid-review. Each moved window ends within 7 days of the Up anyway. Production
-- never takes this Down: the way back from the flag is the "off" commit and a deploy (ADR 0031 §8),
-- and this schema serves that binary as it is.
GRANT EXECUTE ON FUNCTION public.identity_create_for_signup(uuid, uuid) TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_function_privilege('tappa_app', 'public.identity_create_for_signup(uuid,uuid)'::pg_catalog.regprocedure, 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00083 Down: tappa_app did not get identity_create_for_signup back';
    END IF;
END
$$;
-- +goose StatementEnd
