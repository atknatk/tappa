-- 00093 -- T216: tenants_name_keeps_its_isolate, the business name's character rule in the schema.
--
-- THE RULE IS THE APP'S, NOT A NEW ONE. internal/domain/tenant's NameBreaksIsolation (T97, T216)
-- refuses a new or changed business name carrying a bidi embedding, override or isolate
-- (U+202A-U+202E, U+2066-U+2069), a control (Cc: U+0000-U+001F, U+007F-U+009F) or the line or
-- paragraph separator (U+2028, U+2029) -- what lets a name out of the isolate an operator screen puts
-- around it. Until now the rule lived in the two writers alone (signup.ValidateBusiness before
-- CreateTenant, tenant.Accounts.Save before UpdateTenantAccount). The CHECK is the same set, one
-- bracket expression of code point ranges; U+0000 is outside it because text cannot hold it at all.
-- internal/db's tenantname_test.go evaluates the stored predicate over every code point and holds it
-- equal to the Go rule, and drives the table itself on each of the set's runes.
--
-- WHAT IT ALSO CLOSES, AND ONLY THAT: the operator's tenant search (op_read_tenants, 00063) matches
-- the STORED name while the screens print shownName's -- the set dropped. Once every stored name is
-- outside the set, shownName drops nothing, so a name stored as "Rusty" RLO "Bar" can no longer show
-- as RustyBar and be missed by that search; op_read_tenants is not redefined. Runes OUTSIDE the set
-- that render as nothing (U+200B, U+00AD, the joiners and implicit marks) and white space a browser
-- collapses still make the shown and the stored text differ -- the writers accept them on purpose
-- (backlog T250).
--
-- WHAT THE UP DOES ON A DATABASE THAT STILL HAS ONE (a name stored before T97 reached the writers, or
-- written by hand). Accounts.Save keeps an UNCHANGED name on purpose (its inline check, for refuseHiddenName's reason), so such a
-- row can exist; a CHECK, though, binds every row version an UPDATE writes, whatever column changed
-- -- that tenant could no longer move its time zone or be suspended. So the constraint is never left
-- behind on data it does not hold: the DO block counts those names and refuses with the number
-- alone (no id, no name -- the name is the tenant's text), and the file changes nothing. Renaming a
-- tenant is not a migration's to do. In the deploy that is the migrate Job failing (backoffLimit 2)
-- and the rollout stopping; the old pod keeps serving on the schema before this file.
--
-- WHY NOT VALID AND VALIDATE IN ONE TRANSACTION, NOT TWO FILES (00065/00066's split). The split
-- exists so the VALIDATE's scan runs under SHARE UPDATE EXCLUSIVE instead of the ADD's ACCESS
-- EXCLUSIVE, which in one transaction is held to the commit. Two reasons to accept that here:
-- tenants holds dozens of rows in production (00029's note), so the scan is the lock's least part;
-- and a split's refusal would leave a committed NOT VALID constraint behind -- the state the
-- paragraph above refuses, on the old binary. lock_timeout is the first statement (00066's rule for
-- a table every tap reads): behind an open transaction on tenants the ADD gives up with 55P03
-- instead of queueing every later reader behind its ACCESS EXCLUSIVE, and the Job's retry runs the
-- whole file again.
--
-- THE COUNT AND RLS. tenants is FORCE ROW LEVEL SECURITY, so a role without BYPASSRLS would count 0;
-- VALIDATE scans regardless of RLS and would still refuse (PG's own 23514, which names no row), so
-- the count shapes the message and not the safety. The migration role is a superuser (deploy/README).
--
-- THE OLD BINARY ON THIS SCHEMA. main's NameBreaksIsolation is this set (read, b0a885e0), so the
-- two writers refuse every name the CHECK refuses before they reach it; the one name they let
-- through untested -- an unchanged one -- cannot be on file once the Up has run.
--
-- THE DOWN drops the constraint (ACCESS EXCLUSIVE, briefly, behind the same lock_timeout).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
DECLARE
    n bigint;
BEGIN
    SELECT count(*) INTO n FROM public.tenants
     WHERE name ~ '[\u0001-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069]';
    IF n > 0 THEN
        RAISE EXCEPTION 'migration 00093: % tenants rows carry a name with a bidi control, a control character or a line or paragraph separator; tenants_name_keeps_its_isolate was not added. Nothing was changed.', n
            USING ERRCODE = 'check_violation';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE tenants ADD CONSTRAINT tenants_name_keeps_its_isolate
    CHECK (name !~ '[\u0001-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069]') NOT VALID;

ALTER TABLE tenants VALIDATE CONSTRAINT tenants_name_keeps_its_isolate;

-- +goose Down
SET LOCAL lock_timeout = '1s';

ALTER TABLE tenants DROP CONSTRAINT tenants_name_keeps_its_isolate;
