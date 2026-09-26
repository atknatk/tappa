-- 00026 -- M10 OP-5: the platform operator's identity, sessions, audit and read
-- tickets, and the five op_* functions that give them a life.
--
-- NORMATIVE SOURCES: docs/adr/0020-platform-operatoru-ayri-kimlik.md (identity) and
-- docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md (the op_* contract). This
-- file implements them; where it had to decide something they left open, the
-- decision is written next to the statement and in docs/plan/m10-platform.md
-- (OP-5 card correction block).
--
-- 🔴 THIS IS CLAUDE.md §4.5 BEING CROSSED ON PURPOSE, IN ONE PLACE (ADR 0021). Every
-- function below is SECURITY DEFINER, owned by tappa_opdefiner (NOLOGIN, BYPASSRLS,
-- NOSUPERUSER, no members), callable by tappa_operator ONLY -- never by PUBLIC, never
-- by tappa_app. None of the five touches a tenant table: they are the operator's own
-- life cycle. The first op_* that reads a tenant row arrives with OP-10/OP-11.
--
-- ============================================================================
-- THE ROLES ARE NOT CREATED HERE -- AND THIS MIGRATION REFUSES TO RUN WITHOUT THEM
-- ============================================================================
-- tappa_operator and tappa_opdefiner live in scripts/db-init/01-roles.sql (ADR 0021
-- §5): roles are cluster objects, and redline R5b fails a BYPASSRLS role written in a
-- migration. But 01-roles.sql runs only on an EMPTY data directory, so a cluster that
-- already exists (the development database, the live cluster) does not have them until
-- the one-time runbook step is applied. The first statement below therefore checks
-- and, if they are missing or carry the wrong attributes, raises with the runbook's
-- name IN THE MESSAGE. That is where it has to be: goose prints the failing statement
-- and then pgconn's PgError.Error(), which is severity + message + SQLSTATE -- a HINT
-- would never reach the migrate Job's log. Measured on the development database with
-- this file and the roles absent (2026-09-26): `goose up` exit 1, the last line
-- `ERROR: migration 00026 (M10 OP-5) needs the cluster role(s) tappa_opdefiner,
-- tappa_operator ... (SQLSTATE 55000)`, goose_db_version 25 before and after. goose
-- runs every migration in its own transaction, so a refusal changes nothing.
--
-- ============================================================================
-- NO TENANT SCOPE -- FOUR WAIVERS, EACH ONE PRINTED BY redline R5 ON EVERY RUN
-- ============================================================================
-- An operator belongs to no tenant (ADR 0020 §1, §5; ADR 0016 §1 is the precedent).
-- A tenant_id column here would have to name SOME tenant, and every candidate makes a
-- customer the owner of the platform's identity or audit. The "target tenant" of an
-- operator act is recorded as a plain attribute (target_tenant_id), deliberately NOT
-- named after the scope column: a column literally called tenant_id would make every
-- derivation in this repository (redline R5, internal/db/rlsforce_test.go,
-- cmd/tappa/insertscope_test.go) treat the table as tenant data and demand the
-- tenant-GUC policy, which is the wrong rule for a table no tenant may read.
-- redline: no-tenant-scope(platform_admins) — platform operatorunun kimligi; hicbir tenant'a ait degil, ADR 0020 §1
-- redline: no-tenant-scope(platform_sessions) — operator oturumu; tenant kapsami yok, omru op_* definer'larinda, ADR 0020 §2
-- redline: no-tenant-scope(operator_audit_log) — operatorun tenant-otesi iz kaydi; tenant'i hedef niteligi olarak tasir, ADR 0020 §5
-- redline: no-tenant-scope(operator_read_tickets) — iki asamali okumanin bileti; tenant kapsami yok, ADR 0021 §2 v
--
-- ⚠️ THE WAIVER SKIPS R5's WHOLE FIVE-CHECK, so what stands in for it is written out
-- and pinned in the live catalogue (internal/db/operatorschema_test.go):
--   * tappa_app: an explicit REVOKE ALL on every table. NOT a no-op: the dev and
--     production default ACL gives a new table `tappa_app=arwd`, a fresh CI database
--     `ar` (ADR 0021 "Bağlam"; re-measured on dev before writing this: pg_default_acl
--     tappa_owner/public/r = {tappa_app=arwd/tappa_owner}). No table here has a
--     sequence (uuid primary keys), so no sequence REVOKE is needed; a test fails if
--     one ever appears.
--   * ROW LEVEL SECURITY, VOLUNTARY (ADR 0021 left it to OP-5): ENABLE + FORCE on all
--     four, with exactly ONE policy in the whole file (tappa_operator's login lookup on
--     platform_admins). Reason, and it is the M8-02 FAZ E measurement: a pg_dump
--     restore re-applies the default ACL to every CREATE TABLE and does not emit the
--     REVOKEs that took it back, so after a restore tappa_app would hold SELECT and
--     INSERT on these tables -- operator password digests, sealed TOTP secrets, live
--     session hashes. With RLS on and no policy for tappa_app, that residue reads 0
--     rows and cannot insert (pinned by a test that re-creates the residue inside a
--     rolled-back transaction). tappa_opdefiner is BYPASSRLS, so the op_* bodies are
--     unaffected.
--   * FORCE AS WELL, AND THE REASON IS A SCRIPT, NOT TASTE: scripts/pg-restore-verify.sh
--     (section 2) refuses a restored database in which the number of RLS-ENABLED tables
--     differs from the number of FORCED ones ("§6 requires both" -- every table in this
--     schema had both until now). An ENABLE-only table here would make every disaster
--     recovery end in "do not put this database into service". The price, stated: the
--     owner is the designated writer of platform_admins (`opadmin` SQL, ADR 0020 §6);
--     on the deployed topology it is a SUPERUSER and FORCE does not bind it, but an
--     owner that is NOT a superuser (a managed-Postgres migration role) would be
--     refused by FORCE with no owner policy -- that topology would need an owner policy
--     or BYPASSRLS on the migration role, decided when it exists.
--
-- ============================================================================
-- CLOCKS: clock_timestamp() ONLY (ADR 0021 §2 vii)
-- ============================================================================
-- Every duration compared and every timestamp written in this file comes from
-- clock_timestamp(): the table DEFAULTs below and every function body. The frozen
-- clocks the ADR bans by name do not appear in any function body or DEFAULT here
-- (catalogue scan, word-bounded, literals included: TestOperator00026_NoFrozenClock).
--
-- ============================================================================
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7)
-- ============================================================================
-- No op_* RAISE formats an argument: a refused call says WHICH function refused and
-- nothing about the token, the hash, the step or the address it was given. (The only
-- formatted RAISEs in this file are the precondition's, which formats the NAMES of the
-- missing roles, and the two server-side LOG lines of section 5, which format a
-- CONSTRAINT name and a SQLSTATE -- never a value. No error an op_* returns carries a
-- DETAIL: constraint errors are caught and replaced, section 5.)

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the two cluster roles exist and have the shape ADR 0021 §1 names.
-- ---------------------------------------------------------------------------
-- The attribute half is not decoration. `IF NOT EXISTS` in 01-roles.sql cannot stop a
-- hand-made role of the same name, and an op_* owned by a SUPERUSER tappa_opdefiner
-- would be a general bypass of every policy in this database with every other check
-- in this file green. Membership is checked for the same reason RoleFacts checks it:
-- a member of tappa_opdefiner is one SET ROLE away from BYPASSRLS.
-- +goose StatementBegin
DO $$
DECLARE
    v_missing text;
BEGIN
    SELECT string_agg(r.name, ', ' ORDER BY r.name)
      INTO v_missing
      FROM unnest(ARRAY['tappa_operator', 'tappa_opdefiner']) AS r(name)
     WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles p WHERE p.rolname = r.name);
    IF v_missing IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The definer's OWN memberships, the direction the check above cannot see
    -- (round-2 audit, measured): inside a rolled-back transaction
    -- `GRANT tappa_owner TO tappa_opdefiner` let this precondition pass while
    -- has_column_privilege(tappa_opdefiner, tags.aes_key_ref / admin_users.password_hash,
    -- SELECT) turned true -- every op_* body would inherit the migration role's reach.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- tappa_operator's own MEMBERS (round-3 audit, measured): inside a rolled-back
    -- transaction `GRANT tappa_operator TO tappa_app` let the checks above pass, turned
    -- has_function_privilege(tappa_app, op_record_auth_event, EXECUTE) from false to
    -- true, and tappa_app called it. Every EXECUTE granted below goes to tappa_operator,
    -- so a member of it holds every op_* -- the one thing ADR 0021 §1 says tappa_app
    -- never gets.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00026 (M10 OP-5): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. platform_admins -- the operator's identity (ADR 0020 §1)
-- ---------------------------------------------------------------------------
-- Only tappa_owner INSERTs here (opadmin SQL, ADR 0020 §6): no application role holds
-- INSERT, and tappa_opdefiner does not either (ADR 0021 §1, pinned).
--
-- password_hash: bcrypt, cost 12..14. The floor is ADR 0020 §1's cost (a weaker digest
-- cannot be stored); the ceiling is 00018's, for 00018's reason (M7-03 A: a cost-31
-- digest makes ONE login attempt cost hours of CPU). Shape as in 00018.
-- totp_secret_sealed: AES-256-GCM envelope sealed by internal/sun under
-- TAPPA_OPERATOR_TOTP_KEK (ADR 0020 §1). The CHECK is a LOWER bound only, derived:
-- 12-byte nonce + ADR 0020's 128-bit minimum secret + 16-byte tag = 44. The exact
-- layout is OP-6's; the chosen 160-bit secret seals to 48 under that layout.
-- totp_last_step: NOT NULL DEFAULT 0 -- ADR 0020 §1 measured that a NULL here makes
-- `totp_last_step < $step` unknown and refuses the FIRST code. 0 is below every real
-- 30-second step (the current one is ~5.9e7).
-- totp_failures / totp_locked_until: the account's own lock state (ADR 0021 §1): the
-- lock is read from HERE, never derived from audit rows.
-- enroll_token_hash: KEYLESS SHA-256 of the raw enrollment token, lower-case hex
-- (ADR 0020 §3, card item 17). The hash is over the token's TEXT exactly as it
-- travels in the link (convert_to(token, 'UTF8')); cmd/opadmin (OP-9) must hash the
-- same bytes. The TTL ceiling is the schema's (ADR 0015 precedent): one hour, above the
-- product TTL of 30 minutes (ADR 0020 §3), so a single clock read in the writer is not
-- a precondition for passing it.
-- ⚠️ THAT CEILING IS ONLY AS GOOD AS enroll_issued_at, AND enroll_issued_at IS WRITTEN
-- BY ITS WRITER: the ADR 0015 precedent kept created_at out of the writer's INSERT list,
-- this table cannot (opadmin IS the owner and issues tokens again on reset-mfa). A
-- writer that put issued_at in the future would move the ceiling with it. Handed to
-- OP-9 by name: cmd/opadmin writes BOTH enroll_issued_at and enroll_expires_at inside
-- the SQL from one clock_timestamp() read, never from a value the CLI computed.
-- 🔴 A PENDING ACCOUNT CARRIES AN UNUSED TOKEN (platform_admins_pending_token_unused).
-- Single use is held by TWO independent layers and each is pinned on its own (round-2
-- audit): op_complete_enrollment's `enroll_used_at IS NULL`, and this CHECK. Without
-- the CHECK, an owner statement that returns a used account to 'pending' WITHOUT
-- issuing a new token (exactly the state a careless reset-mfa produces) re-opens the
-- OLD token as soon as the function's predicate is gone -- measured by the audit: the
-- mutant enrolled the account again with the old token and every other test stayed
-- green. Handed to OP-9 by name: reset-mfa changes enroll_token_hash and sets
-- enroll_used_at = NULL in the SAME statement.
CREATE TABLE platform_admins (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email              citext NOT NULL
                           CHECK (btrim(email::text) <> '' AND char_length(email::text) <= 254),
    display_name       text NOT NULL
                           CHECK (btrim(display_name) <> '' AND char_length(display_name) <= 200),
    status             text NOT NULL DEFAULT 'pending'
                           CHECK (status IN ('pending', 'active', 'disabled')),
    password_hash      text
                           CHECK (password_hash ~ '^\$2[aby]\$1[2-4]\$[./A-Za-z0-9]{53}$'),
    totp_secret_sealed bytea
                           CHECK (octet_length(totp_secret_sealed) >= 44),
    totp_last_step     bigint NOT NULL DEFAULT 0 CHECK (totp_last_step >= 0),
    totp_failures      integer NOT NULL DEFAULT 0 CHECK (totp_failures >= 0),
    totp_locked_until  timestamptz,
    last_login_at      timestamptz,
    enroll_token_hash  text CHECK (enroll_token_hash ~ '^[0-9a-f]{64}$'),
    enroll_issued_at   timestamptz,
    enroll_expires_at  timestamptz,
    enroll_used_at     timestamptz,
    created_at         timestamptz NOT NULL DEFAULT clock_timestamp(),

    -- GLOBAL uniqueness (ADR 0020 §1: an allow-list is only worth its key's
    -- uniqueness). citext's btree makes it case-insensitive.
    CONSTRAINT platform_admins_email_key UNIQUE (email),
    -- ADR 0020 §1: an operator without MFA cannot be "active" at the schema level.
    CONSTRAINT platform_admins_active_has_credentials
        CHECK (status <> 'active'
               OR (password_hash IS NOT NULL AND totp_secret_sealed IS NOT NULL)),
    -- A pending account nobody can enroll is a stuck row; opadmin create/reset-mfa
    -- always issue a token (ADR 0020 §3, §6).
    CONSTRAINT platform_admins_pending_has_token
        CHECK (status <> 'pending' OR enroll_token_hash IS NOT NULL),
    -- The three issuance columns travel together.
    CONSTRAINT platform_admins_enroll_triple
        CHECK ((enroll_token_hash IS NULL) = (enroll_issued_at IS NULL)
               AND (enroll_issued_at IS NULL) = (enroll_expires_at IS NULL)),
    CONSTRAINT platform_admins_enroll_ttl_ceiling
        CHECK (enroll_expires_at <= enroll_issued_at + interval '1 hour'),
    CONSTRAINT platform_admins_enroll_used_needs_token
        CHECK (enroll_used_at IS NULL OR enroll_token_hash IS NOT NULL),
    CONSTRAINT platform_admins_pending_token_unused
        CHECK (status <> 'pending' OR enroll_used_at IS NULL)
);

ALTER TABLE platform_admins ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_admins FORCE ROW LEVEL SECURITY;

-- The ONE policy in this file. tappa_operator reads platform_admins for the login
-- lookup only (Go verifies bcrypt and TOTP -- ADR 0021 §1, limit 11), and only an
-- ACTIVE account can log in, so that is the row set it sees. Two consequences, both
-- intended: (1) the DSN holder of limit 11 reads the digests of active operators, not
-- of disabled ones; (2) a pending or disabled account is structurally
-- indistinguishable from an unknown address to the login handler, which is exactly
-- ADR 0020 §3's "same answer, same time" set.
CREATE POLICY platform_admins_operator_login_lookup ON platform_admins
    FOR SELECT
    TO tappa_operator
    USING (status = 'active');

REVOKE ALL ON platform_admins FROM tappa_app;
GRANT SELECT (id, email, display_name, status, password_hash, totp_secret_sealed,
              totp_locked_until)
    ON platform_admins TO tappa_operator;
-- tappa_opdefiner: READS the state it decides on, WRITES the credential and state
-- columns, and never SELECTs the digest or the sealed secret (ADR 0021 §1 "asla" =
-- a SELECT ban; §3.3 names op_complete_enrollment's UPDATE of them as the write
-- exception). No INSERT (ADR 0020 §6). No DELETE.
GRANT SELECT (id, email, status, totp_last_step, totp_failures, totp_locked_until,
              enroll_token_hash, enroll_expires_at, enroll_used_at)
    ON platform_admins TO tappa_opdefiner;
GRANT UPDATE (password_hash, totp_secret_sealed, totp_last_step, status, last_login_at,
              totp_failures, totp_locked_until, enroll_used_at)
    ON platform_admins TO tappa_opdefiner;

-- ---------------------------------------------------------------------------
-- 2. platform_sessions -- server-side session, 8 h absolute / 30 min idle (ADR 0020 §2)
-- ---------------------------------------------------------------------------
-- token_hash: HMAC-SHA256 under TAPPA_OPERATOR_TOKEN_HMAC_KEY, lower-case hex -- the
-- panel's admin_sessions shape (internal/adminauth/token.go). tappa_operator holds NO
-- privilege here at all (ADR 0021 §1: a role that could SELECT token_hash for a touch
-- listed every live hash -- measured by the security audit).
-- created_at and last_used_at are DEFAULTs and not in the definer's INSERT list: the
-- touch predicate measures both, and a writable clock column voids a ceiling (ADR 0015).
CREATE TABLE platform_sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id        uuid NOT NULL REFERENCES platform_admins (id) ON DELETE RESTRICT,
    token_hash      text NOT NULL CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    mfa_verified_at timestamptz,
    last_used_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at      timestamptz,
    CONSTRAINT platform_sessions_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX platform_sessions_admin_idx ON platform_sessions (admin_id);

ALTER TABLE platform_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_sessions FORCE ROW LEVEL SECURITY;

REVOKE ALL ON platform_sessions FROM tappa_app;
GRANT SELECT (id, admin_id, token_hash, created_at, mfa_verified_at, last_used_at,
              revoked_at)
    ON platform_sessions TO tappa_opdefiner;
GRANT INSERT (admin_id, token_hash, mfa_verified_at) ON platform_sessions TO tappa_opdefiner;
GRANT UPDATE (last_used_at, revoked_at) ON platform_sessions TO tappa_opdefiner;

-- ---------------------------------------------------------------------------
-- 3. operator_audit_log -- every operator act, append-only (ADR 0020 §5)
-- ---------------------------------------------------------------------------
-- Column names are OP-5's (ADR 0020 §5, ADR 0021 §2 v "zorunlu içerik"):
--   kind             what happened. A CLOSED set: the five pre-session failures
--                    (ADR 0021 §1, op_record_auth_event), the two session origins
--                    (login, enrollment) and logout. Every later op_* widens this
--                    CHECK in its own migration.
--   session_id       the operator session the act was done under (session-carrying
--                    kinds); NULL for the pre-session kinds, which claim no actor.
--   actor_admin_id   derived from session_id by the definer, never a parameter.
--   target_admin_id  the account a pre-session failure was ABOUT, resolved by the
--                    definer's own lookup; never the address itself or a hash of it.
--   target_tenant_id / target_scope / page_number / page_size
--                    the target of a tenant-crossing act and CONTENT-FREE page
--                    information for reads (OP-10+). The search term and the keyset
--                    cursor are never stored here, neither raw nor hashed (ADR 0021
--                    §2 v 1). target_tenant_id has NO foreign key: a FK from a
--                    tenant-less table would answer "does this tenant exist" to a
--                    rolled-back write (00020's argument; ADR 0021 B14).
--   detail           reserved; '{}' for every kind born here. CLAUDE.md §7's list and
--                    ADR 0020 §5's never enter it.
--   at               DEFAULT clock_timestamp() and in NOBODY's INSERT list (ADR 0021
--                    O-4: `DEFAULT now()` back-dated a row by the length of the
--                    transaction that wrote it).
CREATE TABLE operator_audit_log (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    at               timestamptz NOT NULL DEFAULT clock_timestamp(),
    kind             text NOT NULL
                         CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed',
                                         'locked', 'enrollment_failed',
                                         'login', 'enrollment', 'logout')),
    session_id       uuid REFERENCES platform_sessions (id) ON DELETE RESTRICT,
    actor_admin_id   uuid REFERENCES platform_admins (id) ON DELETE RESTRICT,
    target_admin_id  uuid REFERENCES platform_admins (id) ON DELETE RESTRICT,
    target_tenant_id uuid,
    target_scope     text CHECK (target_scope ~ '^[a-z][a-z_]{0,62}$'),
    page_number      integer CHECK (page_number >= 1),
    page_size        integer CHECK (page_size BETWEEN 1 AND 200),
    detail           jsonb NOT NULL DEFAULT '{}'::jsonb
                         CHECK (jsonb_typeof(detail) = 'object'),

    -- A pre-session failure claims no actor and carries no session (ADR 0021 §1);
    -- every other kind is done UNDER a session by the operator it resolves to.
    CONSTRAINT operator_audit_log_actor_shape CHECK (
        (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                  'enrollment_failed')
         AND session_id IS NULL AND actor_admin_id IS NULL)
        OR
        (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed')
         AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
    )
);

CREATE INDEX operator_audit_log_at_idx ON operator_audit_log (at);
CREATE INDEX operator_audit_log_session_idx ON operator_audit_log (session_id);

ALTER TABLE operator_audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE operator_audit_log FORCE ROW LEVEL SECURITY;

REVOKE ALL ON operator_audit_log FROM tappa_app;
-- Every column but the key and the clock. No SELECT, no UPDATE, no DELETE for anyone:
-- tappa_operator reads the log only through the two-phase op_read_audit (OP-14).
GRANT INSERT (kind, session_id, actor_admin_id, target_admin_id, target_tenant_id,
              target_scope, page_number, page_size, detail)
    ON operator_audit_log TO tappa_opdefiner;

-- Append-only, the audit_log / legal_documents family's shape (00005 + 00021): a row
-- trigger for UPDATE/DELETE and a statement trigger for TRUNCATE, both on 00005's
-- tappa_forbid_mutation(). Both bind tappa_owner too. NOT an absolute: a superuser can
-- DISABLE a trigger or DROP the table (00005, 00021 say the same of theirs).
CREATE TRIGGER operator_audit_log_append_only
    BEFORE UPDATE OR DELETE ON operator_audit_log
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();

CREATE TRIGGER operator_audit_log_no_truncate
    BEFORE TRUNCATE ON operator_audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- ---------------------------------------------------------------------------
-- 4. operator_read_tickets -- the ticket of a two-phase read (ADR 0021 §2 v)
-- ---------------------------------------------------------------------------
-- Born here, used from OP-10 on. What the ADR fixed is enforced by the SCHEMA and the
-- GRANTs, so OP-10's functions inherit it rather than re-promise it:
--   * ticket_hash = sha256(raw ticket || canonical jsonb of every read parameter),
--     hex. The raw ticket (256 bits) is never stored.
--   * audit_id NOT NULL REFERENCES operator_audit_log: a ticket without its committed
--     audit row cannot exist.
--   * created_at, created_xact and consumed_at are NOT in the definer's INSERT list --
--     DEFAULTs fill them (ADR 0021 §2 v 1, O-2: a definer that could write
--     created_xact = '2' and created_at = +1 year got data in the same transaction).
--   * CHECK (created_xact > '2'): pg_xact_status('1') and ('2') answer 'committed'
--     (ADR 0021 §2 v 4) -- defence in depth behind the missing grant.
--   * lifetime ceiling 60 s against the schema's own created_at (ADR 0015 precedent).
--     ⚠️ For OP-10: created_at and a clock_timestamp()-derived expires_at are two
--     clock reads, so a lifetime of EXACTLY 60 s can miss the ceiling by microseconds;
--     choose a lifetime below it.
--   * the definer UPDATEs consumed_at and nothing else; tappa_operator holds none of
--     the four verbs (ADR 0021 §1: INSERT minted an audit-less ticket, UPDATE reset a
--     consumption -- both measured).
CREATE TABLE operator_read_tickets (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_hash      text NOT NULL CHECK (ticket_hash ~ '^[0-9a-f]{64}$'),
    session_id       uuid NOT NULL REFERENCES platform_sessions (id) ON DELETE RESTRICT,
    kind             text NOT NULL CHECK (kind ~ '^[a-z][a-z_]{0,62}$'),
    target_tenant_id uuid,
    audit_id         uuid NOT NULL REFERENCES operator_audit_log (id) ON DELETE RESTRICT,
    created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    created_xact     xid8 NOT NULL DEFAULT pg_current_xact_id(),
    expires_at       timestamptz NOT NULL,
    consumed_at      timestamptz,
    CONSTRAINT operator_read_tickets_hash_key UNIQUE (ticket_hash),
    CONSTRAINT operator_read_tickets_ttl_ceiling
        CHECK (expires_at <= created_at + interval '60 seconds'),
    CONSTRAINT operator_read_tickets_xact_is_real CHECK (created_xact > '2'::xid8)
);

ALTER TABLE operator_read_tickets ENABLE ROW LEVEL SECURITY;
ALTER TABLE operator_read_tickets FORCE ROW LEVEL SECURITY;

REVOKE ALL ON operator_read_tickets FROM tappa_app;
GRANT SELECT (id, ticket_hash, session_id, kind, target_tenant_id, audit_id,
              created_xact, expires_at, consumed_at)
    ON operator_read_tickets TO tappa_opdefiner;
GRANT INSERT (ticket_hash, session_id, kind, target_tenant_id, audit_id, expires_at)
    ON operator_read_tickets TO tappa_opdefiner;
GRANT UPDATE (consumed_at) ON operator_read_tickets TO tappa_opdefiner;

-- ===========================================================================
-- 5. THE FIVE op_* FUNCTIONS
-- ===========================================================================
-- Every one of them (ADR 0021 §2; pinned in the live catalogue by
-- internal/db/operatorschema_test.go, re-run for every future op_*):
--   * SECURITY DEFINER, OWNER tappa_opdefiner, VOLATILE (each writes);
--   * SET search_path = pg_catalog, pg_temp, and every table is `public.`-qualified
--     (§2 iv: a path of `pg_catalog, public` let the caller's temp table capture the
--     audit INSERT); citext equality as OPERATOR(public.=) (ADR 0002 M6-01);
--   * REVOKE ALL FROM PUBLIC, EXECUTE to tappa_operator only;
--   * a WRITE returns void (§2 v 7). op_touch_session is the named exception: it
--     returns the session's identity to the session gate;
--   * a refusal (dead session, wrong or used token, stale/poisoned step, lock, wrong
--     status) is ONE message per function, SQLSTATE 28000
--     (invalid_authorization_specification), whatever the reason. 28000 is raised by
--     nothing else on these paths, so a test that expects it cannot be satisfied by a
--     missing grant (42501) or a CHECK (23514). The one other code an op_* raises is
--     op_record_auth_event's 22023 for a kind outside its closed set -- a caller bug,
--     not a refusal of an operator.
--   * 🔴 NO ERROR CARRIES AN ARGUMENT OR A ROW VALUE (round-2 audit, measured). A
--     constraint that refuses an argument answers with PostgreSQL's DETAIL line,
--     "Failing row contains (...)" or "Key (token_hash)=(...)", and that line held the
--     written digest, the sealed envelope in hex, the address and the enrollment hash
--     -- to the caller AND to the server's stderr, i.e. the pod log in production. It
--     was also an ORACLE: 23514/23505 can only arise after every WHERE condition held
--     (the row exists, the token matches, the step is fresh), while every other
--     refusal is 28000. The two functions whose ARGUMENTS reach a constraint
--     (op_open_session: the session hash; op_complete_enrollment: digest, envelope,
--     session hash) therefore catch integrity_constraint_violation (class 23) and turn
--     it into their one refusal: the exception handler rolls back everything the
--     statement did and the caught error is not logged by the server. What is left is
--     a LOG line naming the CONSTRAINT and the SQLSTATE -- never a value -- so a shape
--     bug in the Go caller stays diagnosable.
--     ⚠️ THAT LOG LINE IS NOT SERVER-ONLY, AND IT IS STILL THE ORACLE (round-3 audit,
--     measured): client_min_messages is a USER setting, so a caller that runs
--     `SET LOCAL client_min_messages = log` receives it -- and it exists only when every
--     other condition held (the conditions-held call printed it, the same bad hash
--     with a stale step did not). The DETAIL and the value leak are closed; the
--     "conditions held" bit is not, and it is counted with ADR 0021 limit 14, which
--     reveals the same bit through a SAVEPOINT. Measured and NOT taken: pinning
--     `SET client_min_messages = error` on the function stops the line reaching the
--     caller (the server still logs it), but it closes one of two equivalent channels,
--     reduces nothing a DSN holder can learn, and would change the one-entry proconfig
--     pin ADR 0021 §6 makes normative. Chosen over a shape pre-check,
--     and measured against it: a pre-check is a SECOND copy of each CHECK's regex that
--     drifts silently (and the leak returns the day it does), and it cannot cover a
--     duplicate session hash (23505) without a read-then-write race. The other three
--     functions have no argument that reaches a constraint (op_touch_session and
--     op_close_session use theirs only in a WHERE; op_record_auth_event validates its
--     kind before writing and uses the address and the id only in a WHERE); a test
--     drives them with hostile arguments and pins that nothing value-bearing comes
--     back.
--
-- WHICH WRITE AUDITS WHAT: op_record_auth_event writes one failure row per call;
-- op_open_session and op_complete_enrollment write the session's ORIGIN row in the
-- same statement that creates the session; op_close_session writes the logout row in
-- the same statement that revokes it. op_touch_session writes last_used_at and NO
-- audit row: it is called by every other op_* (§2 i), so a row here would put two rows
-- behind every accepted act and break OP-11's "exactly one row per accepted read".
--
-- A REFUSED CALL LEAVES NO ROW (§2 v 9): the RAISE rolls the statement back, audit
-- INSERT included. The decision this migration records for ADR 0021's open item: Go
-- does NOT write a separate-transaction row for a refused op_* call either -- see the
-- OP-5 card correction for the measured reasoning.
--
-- TOTP LOCK: N = 5 failures, window = 15 minutes. ADR 0020 leaves the numbers to
-- OP-6/OP-8; the MECHANISM is OP-5's and needs numbers to exist. Provisional, changed
-- by a later CREATE OR REPLACE; the pair appears in op_record_auth_event (sets the
-- window when the counter reaches N) and op_open_session (refuses while the counter is
-- at N or above and the window has not passed), and one behaviour test pins that the
-- two agree (a drift in either direction turns it red).

-- ---------------------------------------------------------------------------
-- 5.1 op_touch_session -- THE session predicate (ADR 0021 §2 i, ADR 0020 §2)
-- ---------------------------------------------------------------------------
-- One UPDATE: validity (absolute 8 h from birth, idle 30 min, MFA stamped, not
-- revoked, operator active) and the last_used_at advance are the same statement;
-- zero rows = refusal. The row lock it takes is what serialises a concurrent
-- op_close_session on the same session.
-- +goose StatementBegin
CREATE FUNCTION public.op_touch_session(p_session text,
                                        OUT session_id uuid, OUT admin_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    UPDATE public.platform_sessions AS s
       SET last_used_at = clock_timestamp()
      FROM public.platform_admins AS a
     WHERE s.token_hash = p_session
       AND a.id = s.admin_id
       AND a.status = 'active'
       AND s.revoked_at IS NULL
       AND s.mfa_verified_at IS NOT NULL
       AND s.created_at > clock_timestamp() - interval '8 hours'
       AND s.last_used_at > clock_timestamp() - interval '30 minutes'
    RETURNING s.id, s.admin_id INTO session_id, admin_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_touch_session: operator session refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_touch_session(text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_touch_session(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_touch_session(text) TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 5.2 op_record_auth_event -- sessionless exception 1 (ADR 0021 §1)
-- ---------------------------------------------------------------------------
-- A closed set of FAILURE kinds, no actor claim, no address stored. The account it is
-- about is resolved HERE: by address (citext, case-insensitive) when one is given,
-- otherwise by the account id the caller holds (the enrollment link's id, the
-- intermediate login cookie's id). A match stores the id as target_admin_id; no match
-- stores nothing about the input -- in particular an id that does not exist is not a
-- foreign-key error, so the function's RESULT is the same for a match and a miss.
-- ⚠️ ITS SIDE EFFECTS ARE NOT (round-4 audit, measured): a match makes the
-- target_admin_id foreign-key check run, and the public statistics views count it --
-- pg_stat_xact_user_tables showed seq_scan +1 for an unknown address and +2 for a
-- pending or a disabled account's address, inside a SAVEPOINT that was then rolled
-- back. So the function IS an existence oracle for addresses of accounts the login
-- lookup hides; ADR 0021 limit 15 counts it (not closable from a definer). The KIND is
-- the caller's claim; the TARGET is the database's own fact.
-- totp_failed bumps the account's counter IN THE SAME STATEMENT that writes its row
-- (ADR 0021 §1, 3rd-round addendum), so every increment leaves a row; reaching N opens
-- the lock window. RETURNS void: it does not say whether the address belongs to an
-- operator.
-- THE COUNTER ONLY MOVES ON AN ACTIVE ACCOUNT (round-2 audit, measured). Unfiltered,
-- the UPDATE row-locked a PENDING or DISABLED account too, so a DSN holder that kept
-- one such call open made a second caller wait (55P03 under a lock_timeout) while an
-- unknown id returned at once -- a lock-contention oracle for exactly the accounts the
-- login lookup's RLS policy hides from tappa_operator. Only an active account can be
-- locked out of a login, so the filter costs nothing; active accounts remain
-- detectable this way, and tappa_operator can SELECT those anyway.
-- THE KIND IS GO'S VIEW, THE TARGET IS THE DATABASE'S: Go sees only active accounts
-- (the policy), so it reports a pending or disabled operator's address as
-- 'unknown_email' -- and the row still carries that account's id, because the lookup
-- here sees every status. "unknown_email with a target" therefore reads "an address of
-- an account that cannot log in"; "unknown_email without one" reads "no such account".
-- LOCK SHAPE, deliberate (pinned): the counter resets ONLY on a successful login. Once
-- it has reached N, every further failure re-opens the full window -- after the window
-- has passed, ONE failure locks again; while locked, a failure extends the lock. After
-- the first lock that is at most one guess per window (96 a day at 15 minutes), where
-- resetting the counter when the window ends would allow N per window (480 a day).
-- ⚠️ Counted limit (ADR 0021 limit 7): the DSN holder can call this to write noise and
-- to lock an account; each increment leaves a totp_failed row.
-- +goose StatementBegin
CREATE FUNCTION public.op_record_auth_event(p_kind text,
                                            p_email text DEFAULT NULL,
                                            p_admin uuid DEFAULT NULL)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF p_kind IS NULL OR p_kind NOT IN ('login_failed', 'unknown_email', 'totp_failed',
                                        'locked', 'enrollment_failed') THEN
        RAISE EXCEPTION 'op_record_auth_event: kind is not a pre-session failure kind'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    WITH target AS (
        SELECT a.id
          FROM public.platform_admins AS a
         WHERE (p_email IS NOT NULL AND a.email OPERATOR(public.=) p_email::public.citext)
            OR (p_email IS NULL AND a.id = p_admin)
    ), counted AS (
        UPDATE public.platform_admins AS a
           SET totp_failures     = a.totp_failures + 1,
               totp_locked_until = CASE WHEN a.totp_failures + 1 >= 5
                                        THEN clock_timestamp() + interval '15 minutes'
                                        ELSE a.totp_locked_until
                                   END
          FROM target AS t
         WHERE p_kind = 'totp_failed'
           AND a.id = t.id
           AND a.status = 'active'
        RETURNING a.id
    )
    INSERT INTO public.operator_audit_log (kind, target_admin_id)
    SELECT p_kind, (SELECT t.id FROM target AS t);
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_record_auth_event(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_record_auth_event(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_record_auth_event(text, text, uuid) TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 5.3 op_open_session -- sessionless exception 2 (ADR 0021 §1, ADR 0020 §3)
-- ---------------------------------------------------------------------------
-- ONE statement: the TOTP step advance (replay protection: strictly greater than the
-- last accepted step -- the mirror of §4.4), the step's binding to the wall clock
-- (cur-1 .. cur+1, cur = floor(epoch(clock_timestamp()) / 30); an unbound step was a
-- permanent-lock primitive, ADR 0021 4th round), the lock condition, the last-login
-- stamp and the counter reset are ONE UPDATE; the session row (MFA stamped) and its
-- origin row ('login') are born from the row that UPDATE returns. Zero rows = refusal,
-- no session, no row. The definer cannot verify the password or the code (the TOTP KEK
-- is in the process -- ADR 0020 §3); it records that the process vouched (limit 1).
-- +goose StatementBegin
CREATE FUNCTION public.op_open_session(p_admin uuid, p_session_hash text, p_totp_step bigint)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_rows       bigint;
    v_constraint text;
    v_state      text;
BEGIN
    BEGIN
        WITH cur AS (
            SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint AS step
        ), advanced AS (
            UPDATE public.platform_admins AS a
               SET totp_last_step    = p_totp_step,
                   last_login_at     = clock_timestamp(),
                   totp_failures     = 0,
                   totp_locked_until = NULL
              FROM cur
             WHERE a.id = p_admin
               AND a.status = 'active'
               AND a.totp_last_step < p_totp_step
               AND p_totp_step BETWEEN cur.step - 1 AND cur.step + 1
               AND (a.totp_failures < 5 OR a.totp_locked_until <= clock_timestamp())
            RETURNING a.id
        ), born AS (
            INSERT INTO public.platform_sessions (admin_id, token_hash, mfa_verified_at)
            SELECT advanced.id, p_session_hash, clock_timestamp()
              FROM advanced
            RETURNING platform_sessions.id, platform_sessions.admin_id
        )
        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id)
        SELECT 'login', born.id, born.admin_id
          FROM born;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_open_session: an argument failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_open_session: session refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_open_session(uuid, text, bigint) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_open_session(uuid, text, bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_open_session(uuid, text, bigint) TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 5.4 op_complete_enrollment -- sessionless exception 3 (ADR 0021 §1, ADR 0020 §3)
-- ---------------------------------------------------------------------------
-- ONE conditional statement (ADR 0015's single-pass consumption): id AND keyless
-- SHA-256 of the RAW token match, the account is pending, the token is unused and not
-- expired by the wall clock, the first code's step is inside cur-1 .. cur+1 -- then the
-- token is consumed, the digest and the sealed secret are WRITTEN (not read: the
-- definer holds no SELECT on either), totp_last_step becomes the first code's step (so
-- the enrollment code cannot open a session: op_open_session wants a GREATER step), the
-- account turns active, and the first session and its origin row ('enrollment') are
-- born from the returned row. Zero rows = ONE refusal for every reason (expired, used,
-- wrong id/hash, not pending, poisoned step, or an argument a constraint refuses --
-- §2 v 7's scope note and section 5's constraint handler). N concurrent calls
-- with the same token: the row lock plus the re-checked WHERE give exactly one winner.
-- +goose StatementBegin
CREATE FUNCTION public.op_complete_enrollment(p_admin uuid,
                                              p_token text,
                                              p_password_hash text,
                                              p_totp_sealed bytea,
                                              p_totp_step bigint,
                                              p_session_hash text)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_rows       bigint;
    v_constraint text;
    v_state      text;
BEGIN
    BEGIN
        WITH cur AS (
            SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint AS step
        ), enrolled AS (
            UPDATE public.platform_admins AS a
               SET enroll_used_at     = clock_timestamp(),
                   password_hash      = p_password_hash,
                   totp_secret_sealed = p_totp_sealed,
                   totp_last_step     = p_totp_step,
                   status             = 'active',
                   last_login_at      = clock_timestamp(),
                   totp_failures      = 0,
                   totp_locked_until  = NULL
              FROM cur
             WHERE a.id = p_admin
               AND a.status = 'pending'
               AND a.enroll_token_hash = encode(sha256(convert_to(p_token, 'UTF8')), 'hex')
               AND a.enroll_used_at IS NULL
               AND a.enroll_expires_at > clock_timestamp()
               AND p_totp_step BETWEEN cur.step - 1 AND cur.step + 1
            RETURNING a.id
        ), born AS (
            INSERT INTO public.platform_sessions (admin_id, token_hash, mfa_verified_at)
            SELECT enrolled.id, p_session_hash, clock_timestamp()
              FROM enrolled
            RETURNING platform_sessions.id, platform_sessions.admin_id
        )
        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id)
        SELECT 'enrollment', born.id, born.admin_id
          FROM born;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_complete_enrollment: an argument failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_complete_enrollment: enrollment refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_complete_enrollment(uuid, text, text, bytea, bigint, text)
    OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_complete_enrollment(uuid, text, text, bytea, bigint, text)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_complete_enrollment(uuid, text, text, bytea, bigint, text)
    TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 5.5 op_close_session -- logout (ADR 0021 §1, ADR 0020 §2)
-- ---------------------------------------------------------------------------
-- Resolves the session through op_touch_session (the ONE predicate; §2 i), then
-- revokes it and writes the logout row in ONE statement. A second close of the same
-- session is refused by the touch (revoked_at is set), so a session has at most one
-- logout row.
-- +goose StatementBegin
CREATE FUNCTION public.op_close_session(p_session text)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_rows    bigint;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    WITH revoked AS (
        UPDATE public.platform_sessions AS s
           SET revoked_at = clock_timestamp()
         WHERE s.id = v_session
           AND s.revoked_at IS NULL
        RETURNING s.id, s.admin_id
    )
    INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id)
    SELECT 'logout', revoked.id, revoked.admin_id
      FROM revoked;

    GET DIAGNOSTICS v_rows = ROW_COUNT;
    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_close_session: logout refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_close_session(text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_close_session(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_close_session(text) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DESTROYS, named rather than left to be discovered (00021's
-- tradition): every operator account, every operator session and THE WHOLE OPERATOR
-- AUDIT TRAIL. DROP TABLE is not stopped by the append-only triggers (00021 measured
-- the same of its own). This is a development tool; on a cluster where operators
-- exist it erases the evidence of what they did. The two roles are NOT dropped -- they
-- are cluster objects owned by scripts/db-init/01-roles.sql -- and every grant made
-- above disappears with the objects it was made on.
DROP FUNCTION IF EXISTS public.op_close_session(text);
DROP FUNCTION IF EXISTS public.op_complete_enrollment(uuid, text, text, bytea, bigint, text);
DROP FUNCTION IF EXISTS public.op_open_session(uuid, text, bigint);
DROP FUNCTION IF EXISTS public.op_record_auth_event(text, text, uuid);
DROP FUNCTION IF EXISTS public.op_touch_session(text);
DROP TABLE IF EXISTS operator_read_tickets;
DROP TABLE IF EXISTS operator_audit_log;
DROP TABLE IF EXISTS platform_sessions;
DROP TABLE IF EXISTS platform_admins;
