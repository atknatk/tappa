-- 00040 -- M11 API-1a (handed over by the API-1 review): goose's ledger, goose_db_version, is
-- the migration role's alone. tappa_app loses every privilege on it and on its sequence.
--
-- WHY (measured by the API-1 security review, 2026-10-13; reproduced for this file): goose
-- creates goose_db_version BEFORE the first migration, so the table carries whatever default
-- ACL the database had then -- `ar` on a fresh production volume (scripts/db-init/01-roles.sql),
-- `arwd` on production's live database and on every development and CI volume
-- (03-dev-production-default-acl.sql). EITHER ONE IS A HOLE, and the narrow one is not the
-- smaller: INSERT alone lets tappa_app forge the row of a migration that has not run. Measured:
-- as tappa_app, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (<next>, true)`,
-- then `goose up` as the owner answered "no migrations to run" and the migration never ran --
-- a security fix shipped as a migration would be skipped silently, and the ledger would say it
-- was applied. UPDATE and DELETE (the wide default) rewrite or erase the record of what ran.
-- The application never reads the ledger: its readers are goose (connected as the migration
-- role, tappa_owner), scripts/pg-backup.sh and scripts/pg-restore-verify.sh (both PGUSER
-- tappa_owner) and the test suite (the owner connection); no statement in db/queries, no Go
-- package and no script reads it as tappa_app (counted for this file; the report lists them).
--
-- THE SEQUENCE TOO: goose_db_version.id draws from goose_db_version_id_seq, which carries the
-- sequence default (`rU` for tappa_app). A table REVOKE does not reach it; nextval() from the
-- application would move the ledger's ids. It is the ledger's, so it goes with it.
--
-- THE CATALOGUE TEST'S WITNESS MOVES (TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd):
-- it used to read tappa_app's UPDATE and DELETE on THIS table as the proof that the chain ran
-- under production's `arwd` default. After this file that privilege is gone everywhere, so the
-- test reads the default ACL itself and the order in which it and goose's first ledger row were
-- written (internal/db/m11schema_test.go says how).
--
-- After the REVOKEs a postcondition measures the result as tappa_app would meet it
-- (has_table_privilege / has_sequence_privilege count role membership and PUBLIC): a privilege
-- reaching tappa_app through another role or through PUBLIC is not something a REVOKE here can
-- take back, so the migration stops and names it instead of shipping a ledger that only looks
-- closed.
--
-- No table, function or role is created; nothing is granted to anybody.

-- +goose Up

REVOKE ALL ON TABLE goose_db_version FROM tappa_app;
REVOKE ALL ON SEQUENCE goose_db_version_id_seq FROM tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'TRUNCATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'REFERENCES')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.goose_db_version', 'TRIGGER')
       OR pg_catalog.has_sequence_privilege('tappa_app', 'public.goose_db_version_id_seq', 'USAGE')
       OR pg_catalog.has_sequence_privilege('tappa_app', 'public.goose_db_version_id_seq', 'SELECT')
       OR pg_catalog.has_sequence_privilege('tappa_app', 'public.goose_db_version_id_seq', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00040 (M11 API-1a) revoked tappa_app''s privileges on goose_db_version and its sequence, and tappa_app still reaches them -- through PUBLIC or a role it is a member of. A REVOKE here cannot take that back: revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- 🔴 THE Down KEEPS THE LEDGER CLOSED, AND THAT IS THE DECISION, NOT AN OMISSION.
-- A Down exists so that the schema at 00039 serves the code of 00039. Nothing at 00039 needs
-- tappa_app on the ledger -- the application never reads it (the Up's header counts the
-- readers) -- so there is nothing to give back for the code's sake, and giving back the
-- privilege would re-open, on a rollback, the hole the Up closed: INSERT alone lets a stolen
-- application credential mark a future migration as applied. The repository's other Downs
-- give back the previous state exactly (00024, 00021); here the previous state is not even
-- one state -- `ar` or `arwd`, whichever default the database was born under -- and neither is
-- a state anybody chose. So the Down states the closed ledger once more: idempotent on a
-- database the Up left closed, and it closes one that somebody re-opened by hand since.
REVOKE ALL ON TABLE goose_db_version FROM tappa_app;
REVOKE ALL ON SEQUENCE goose_db_version_id_seq FROM tappa_app;
