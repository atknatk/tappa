-- 00061 -- T138: the nine pre-M11 trigger functions lose PostgreSQL's default EXECUTE to PUBLIC.
--
-- WHAT. tappa_forbid_mutation, tappa_forbid_counter_rewind, tappa_forbid_revocation_reset,
-- tappa_forbid_app_key_ref_rewrite, tappa_forbid_encoded_at_at_insert, tappa_forbid_encoded_at_rewrite,
-- tappa_forbid_unencoded_activation, tappa_check_transaction_review and tappa_audit_at_is_the_wall_clock
-- were made (00005 … 00033) before ADR 0028 §1's rule -- every new function EXECUTE-to-its-owner,
-- from 00037 on -- and were born with CREATE FUNCTION's default: EXECUTE to PUBLIC, i.e. to every
-- role. Their files are deployed and are not changed (CLAUDE.md §6); this file takes the grant away.
-- After it, the nine are like every M11 trigger function: EXECUTE in their owner alone. T137's
-- catalogue (internal/db/privcatalog_test.go) held the nine lines `execute=public,…` as a known,
-- accepted exception; they now read `execute=-`.
--
-- WHY IT IS HYGIENE AND NOT A FIX (T137, second round, two lenses). All nine are SECURITY INVOKER
-- trigger functions: a direct call is refused by PostgreSQL ("trigger functions can only be called as
-- triggers"), tappa_app cannot bind one to a real table (not the owner, no TRIGGER privilege), and one
-- bound to its own temporary table runs with its own rights. What the REVOKE closes is that last
-- path itself: without EXECUTE, CREATE TRIGGER … EXECUTE FUNCTION public.tappa_forbid_mutation() on
-- tappa_app's temporary table is refused (42501) -- measured on 17.10, pinned in
-- internal/db/triggerexecute_test.go.
--
-- 🔴 THE TRIGGERS KEEP FIRING. EXECUTE on a trigger function is checked when the trigger is CREATED,
-- never when it FIRES: the role whose write fires it needs no EXECUTE. Measured on PostgreSQL 17.10
-- (2026-10-19): with PUBLIC's EXECUTE revoked, tappa_app's UPDATE and DELETE of a row bound to
-- tappa_forbid_mutation are refused by the trigger (23001 "append-only table …"), while its direct
-- call is refused for the privilege (42501). If that were wrong this file would switch §4.3's
-- append-only guard off for every role but the owner -- so it is not taken on trust:
-- internal/db/triggerexecute_test.go fires each of the nine, after this file, as a role that may
-- NOT execute it (has_function_privilege false, asserted in the same savepoint), and reads the
-- trigger's own refusal (its message, not only its SQLSTATE) or, for the clock, its effect.
-- TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger measured the same for the clock in 00033.
--
-- WHO MAY STILL BIND THEM. The owner -- the migration role, which made them and runs every later
-- migration's CREATE TRIGGER (00020, 00021, 00033, 00048 … reuse tappa_forbid_mutation): an owner's
-- EXECUTE is implicit and is not touched by a REVOKE … FROM PUBLIC. The Down below asserts it.
--
-- THE DOWN DOES NOT GIVE PUBLIC ITS EXECUTE BACK (option (b) of T138's brief; the reasoning):
--   * what the Down would restore is a privilege no code uses and every reviewer has marked as one
--     to remove; a rollback that re-opens it is a security regression bought for symmetry;
--   * a `GRANT … TO PUBLIC` in any migration -- Down included -- is a redline R5b FAIL (T137): the
--     other option needs a waiver for that exact statement in this exact file, and T130's waiver
--     for 00058's Down took three rounds of mutants to make narrow. A waiver is a door in the one
--     rule that says PUBLIC is never granted anything; the Down is not worth one;
--   * the application at 00060 does not depend on PUBLIC's EXECUTE (nothing calls these functions;
--     the triggers fire without it -- above), so the database a Down leaves is a correct 00060
--     database: every earlier Down and Up that binds one of the nine runs as the owner;
--   * CLAUDE.md §6 ("Migration geri alınabilir olmalı", Down filled): the Down is not empty
--     -- it asserts the state it leaves (the nine exist, the migration role may still EXECUTE and so
--     bind them), and `goose down` + `goose up` is a clean cycle because the Up is idempotent: its
--     precondition asks only that the nine exist as INVOKER trigger functions, not that PUBLIC still
--     holds EXECUTE (a REVOKE of a privilege not held is a no-op), and its postcondition reads the
--     whole ACL either way. TestTriggerExecute00061_DownKeepsTheRevokeAndUpIsIdempotent walks it.
--
-- lock_timeout 1 s, THE FIRST STATEMENT (deploy/README.md's rule). A REVOKE rewrites the functions'
-- pg_proc rows and takes no table lock; the setting bounds the wait anyway, should a concurrent
-- CREATE OR REPLACE of one of the nine hold its row.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- PRECONDITION: each of the nine exists in public by its exact signature, returns trigger and is
-- SECURITY INVOKER (a DEFINER function's EXECUTE would be a different question, not this file's).
-- +goose StatementBegin
DO $$
DECLARE
    sig text;
BEGIN
    FOREACH sig IN ARRAY ARRAY[
        'public.tappa_forbid_mutation()', 'public.tappa_forbid_counter_rewind()',
        'public.tappa_forbid_revocation_reset()', 'public.tappa_forbid_app_key_ref_rewrite()',
        'public.tappa_forbid_encoded_at_at_insert()', 'public.tappa_forbid_encoded_at_rewrite()',
        'public.tappa_forbid_unencoded_activation()', 'public.tappa_check_transaction_review()',
        'public.tappa_audit_at_is_the_wall_clock()']
    LOOP
        IF pg_catalog.to_regprocedure(sig) IS NULL THEN
            RAISE EXCEPTION 'migration 00061 (T138) needs % (00005 … 00033). Migrate in order. Nothing was changed.', sig
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                        WHERE p.oid = pg_catalog.to_regprocedure(sig)
                          AND p.prorettype = 'pg_catalog.trigger'::pg_catalog.regtype
                          AND NOT p.prosecdef) THEN
            RAISE EXCEPTION 'migration 00061 (T138): % is not a SECURITY INVOKER trigger function. Nothing was changed.', sig
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION public.tappa_forbid_mutation() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_forbid_counter_rewind() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_forbid_revocation_reset() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_forbid_app_key_ref_rewrite() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_forbid_encoded_at_at_insert() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_forbid_encoded_at_rewrite() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_forbid_unencoded_activation() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_check_transaction_review() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION public.tappa_audit_at_is_the_wall_clock() FROM PUBLIC;

-- POSTCONDITION: each ACL's EXECUTE grantees are its owner and nobody else -- read from the ACL
-- itself (a NULL proacl is the default, spelled out), so an explicit grant to a role, which a REVOKE
-- … FROM PUBLIC does not reach, refuses the file instead of passing it.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1
                 FROM pg_catalog.pg_proc p
                CROSS JOIN LATERAL pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS a
                WHERE p.oid IN ('public.tappa_forbid_mutation()'::pg_catalog.regprocedure,
                                'public.tappa_forbid_counter_rewind()'::pg_catalog.regprocedure,
                                'public.tappa_forbid_revocation_reset()'::pg_catalog.regprocedure,
                                'public.tappa_forbid_app_key_ref_rewrite()'::pg_catalog.regprocedure,
                                'public.tappa_forbid_encoded_at_at_insert()'::pg_catalog.regprocedure,
                                'public.tappa_forbid_encoded_at_rewrite()'::pg_catalog.regprocedure,
                                'public.tappa_forbid_unencoded_activation()'::pg_catalog.regprocedure,
                                'public.tappa_check_transaction_review()'::pg_catalog.regprocedure,
                                'public.tappa_audit_at_is_the_wall_clock()'::pg_catalog.regprocedure)
                  AND a.privilege_type = 'EXECUTE'
                  AND a.grantee <> p.proowner) THEN
        RAISE EXCEPTION 'migration 00061 (T138): a role other than its owner still holds EXECUTE on one of the nine trigger functions. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- PUBLIC's EXECUTE is NOT given back (the header says why). The Down asserts the state it leaves: the
-- nine exist, and the migration role -- which binds them in every earlier Down and Up -- may still
-- EXECUTE them.
-- +goose StatementBegin
DO $$
DECLARE
    sig text;
BEGIN
    FOREACH sig IN ARRAY ARRAY[
        'public.tappa_forbid_mutation()', 'public.tappa_forbid_counter_rewind()',
        'public.tappa_forbid_revocation_reset()', 'public.tappa_forbid_app_key_ref_rewrite()',
        'public.tappa_forbid_encoded_at_at_insert()', 'public.tappa_forbid_encoded_at_rewrite()',
        'public.tappa_forbid_unencoded_activation()', 'public.tappa_check_transaction_review()',
        'public.tappa_audit_at_is_the_wall_clock()']
    LOOP
        IF pg_catalog.to_regprocedure(sig) IS NULL
           OR NOT pg_catalog.has_function_privilege(current_user, pg_catalog.to_regprocedure(sig), 'EXECUTE') THEN
            RAISE EXCEPTION 'migration 00061 Down (T138): % is missing, or the migration role may not EXECUTE it -- the earlier migrations that bind it could not run. Nothing was changed.', sig
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd
