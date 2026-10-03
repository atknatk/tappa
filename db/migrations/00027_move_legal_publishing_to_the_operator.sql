-- 00027 -- M10 OP-10, phase A (the data layer): the legal texts' write path and their
-- version list move onto the operator's op_* surface, and the application role loses
-- its INSERT on legal_documents.
--
-- NORMATIVE SOURCES: docs/adr/0020-platform-operatoru-ayri-kimlik.md §7 (this move:
-- op_publish_legal(session, slug, body), REVOKE INSERT FROM tappa_app, published_by =
-- platform_admins.id, the 256 KiB ceiling), docs/adr/0021-op-fonksiyonlari-tenant-
-- otesi-erisim.md §2 (the op_* contract: the session predicate, two-phase reads, the
-- wall clock) and docs/adr/0016 §3 and §6 (append-only, the ceiling). Where this file
-- had to decide something those left open, the decision is written next to the
-- statement and in ADR 0021's "OP-10 notu".
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape -- 00026's precondition, repeated,
--      because three new SECURITY DEFINER functions are about to belong to
--      tappa_opdefiner;
--   1. widens two closed sets: operator_audit_log.kind gains 'read' and
--      'legal_publish'; operator_read_tickets.kind turns from a SHAPE into the closed
--      set of read kinds (OP-5 left it a shape on purpose -- m10-platform.md, OP-5
--      card correction, item 14 (c));
--   2. legal_documents.published_at's DEFAULT becomes clock_timestamp();
--   3. the grants: tappa_app loses INSERT, tappa_opdefiner gains the exact columns the
--      three functions read and write;
--   4. three functions: op_begin_read (the general first phase of a read),
--      op_read_legal_versions (the first op_read_*) and op_publish_legal (a one-phase
--      write that returns void).
--
-- No table is created here, so there is no redline waiver, no new RLS and no sequence
-- to REVOKE; the table GRANTs below name column lists.
--
-- CLOCKS (ADR 0021 §2 vii): the one comparison the three functions make
-- (expires_at > clock_timestamp()) and the timestamps they write (consumed_at,
-- expires_at, and through DEFAULTs created_at, at and published_at) come from
-- clock_timestamp(). The catalogue scan of ADR 0021 §6 (TestOperator00026_NoFrozenClock)
-- walks every function tappa_opdefiner owns, these three included, for its list of
-- frozen-clock names.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7): the RAISE EXCEPTIONs of the three functions
-- carry constant messages, and their two RAISE LOGs format a constraint name and a
-- SQLSTATE (00026's shape); the one formatted RAISE EXCEPTION in this file is the
-- precondition's, which formats the NAMES of missing roles. The raw read ticket is
-- written to no column -- the ticket row holds sha256(ticket || bound parameters). The
-- two functions whose INSERTs carry values a constraint can refuse (op_begin_read: the
-- ticket row; op_publish_legal: the caller's slug and body) catch
-- integrity_constraint_violation and answer with a fixed message, the 00026 pattern (its
-- section 5 header has the measurement): an uncaught violation's DETAIL is the failing
-- row.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the two roles still have the shape ADR 0021 §1 names.
-- ---------------------------------------------------------------------------
-- 00026 checked this when it ran. It is checked again because the reason is the same
-- and it applies to the objects THIS file creates: an op_* owned by a SUPERUSER, a
-- LOGIN or a member-carrying tappa_opdefiner is a general bypass with every other check
-- green, and a member of tappa_operator inherits EXECUTE on the three functions below.
-- The checks and the order are 00026's; only the migration's name differs.
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
        RAISE EXCEPTION 'migration 00027 (M10 OP-10) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00027 (M10 OP-10): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00027 (M10 OP-10): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00027 (M10 OP-10): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00027 (M10 OP-10): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00027 (M10 OP-10): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00027 (M10 OP-10): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. Two closed sets widen.
-- ---------------------------------------------------------------------------
-- operator_audit_log.kind (00026: "every later op_* widens this CHECK in its own
-- migration"):
--   'read'           written by op_begin_read, ONE row per accepted read (ADR 0021 §2 v
--                    1; OP-11's "exactly one row" reads it). WHAT was read is the row's
--                    target_scope (the read kind, e.g. 'legal_versions') and its
--                    content-free page columns -- so later reads add a read kind, not
--                    an audit kind. ADR 0021 §2 v 1 keeps a later read's search term and
--                    keyset cursor out of this table: they are bound in the ticket hash.
--   'legal_publish'  written by op_publish_legal in the same statement as the version
--                    (ADR 0020 "Audit'in yeri": a legal document belongs to no tenant,
--                    so its trail is this log and no tenant audit_log row is written).
-- Both kinds are session-carrying, so 00026's operator_audit_log_actor_shape already
-- demands a session and an actor for them.
-- The constraint keeps 00026's NAME so the next migration finds it where this one did.
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
    CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                    'enrollment_failed', 'login', 'enrollment', 'logout',
                    'read', 'legal_publish'));
-- A read row without its scope would say "something was read" -- the one fact the row
-- exists to pin down. (Page columns stay optional: a single-object read has no page.)
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_read_has_scope
    CHECK (kind <> 'read' OR target_scope IS NOT NULL);

-- operator_read_tickets.kind: the closed set of read kinds. op_begin_read refuses any
-- other value before writing; this is the schema's copy, which binds the owner too.
ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
    CHECK (kind IN ('legal_versions'));

-- ---------------------------------------------------------------------------
-- 2. legal_documents.published_at: the wall clock, from a DEFAULT.
-- ---------------------------------------------------------------------------
-- ADR 0021 §2 vii: every timestamp an op_* writes comes from clock_timestamp(). 00020's
-- DEFAULT is now(), i.e. the TRANSACTION's start, so a version published by a
-- transaction that had been open for a while would be back-dated by that long (the
-- audit table's own measurement, ADR 0021 O-4: 2.007 s for a transaction held open
-- 2 s).
-- Two ways to honour the rule, and the choice:
--   (a) op_publish_legal writes published_at = clock_timestamp() itself -- the
--       definer then needs INSERT on published_at, i.e. a definer that could write any
--       time at all (ADR 0015's precedent: a writable clock column voids what it
--       measures; 00026 kept created_at, created_xact and at out of every INSERT list
--       for that reason);
--   (b) the DEFAULT is the wall clock and published_at is in NOBODY's INSERT list
--       below -- the 00026 shape, and the one a catalogue scan of DEFAULTs can see.
-- (b) is taken. It also changes what the public read path's tie-break comment in
-- db/queries/legal.sql describes: two versions appended in one transaction no longer
-- share a timestamp (that comment is updated in the same change).
ALTER TABLE legal_documents ALTER COLUMN published_at SET DEFAULT clock_timestamp();

-- ---------------------------------------------------------------------------
-- 3. Grants.
-- ---------------------------------------------------------------------------
-- 🔴 tappa_app LOSES INSERT -- AT TABLE LEVEL, WHICH IS WHAT TAKES THE COLUMN GRANTS
-- WITH IT. 00020 granted INSERT (slug, body, published_by) column by column, so
-- has_table_privilege(tappa_app, legal_documents, INSERT) was ALREADY false while
-- tappa_app could write (ADR 0020 §7's measurement contract: the evidence is
-- has_any_column_privilege(..., 'INSERT') = false). A table-level REVOKE INSERT removes
-- the column-level INSERT grants too (PostgreSQL: revoking a table privilege revokes it
-- on every column as well) -- measured on the development database (2026-10-03,
-- inside BEGIN ... ROLLBACK): before, has_table_privilege = f and
-- has_any_column_privilege = t; after this statement
-- has_column_privilege(tappa_app, legal_documents, slug|body|published_by, INSERT) are
-- all f, and SELECT on id, slug, body, published_at stays t, so the public read path
-- (internal/domain/legal.Store.Refresh) reads as before. The column-level spelling
-- (REVOKE INSERT (slug, body) ...) left has_any_column_privilege = t, measured in the
-- same session.
-- After it, an INSERT into legal_documents as tappa_app -- the M7-06 panel's publish
-- path included -- or as tappa_operator is refused with 42501 (measured:
-- TestOperator00027_TheApplicationCanNoLongerWriteALegalText); the writer this migration
-- adds is op_publish_legal, through tappa_opdefiner's column grant below.
-- ADR 0016 §2b's "no database depth on the write side" debt closes with this line.
REVOKE INSERT ON legal_documents FROM tappa_app;

-- tappa_opdefiner, exactly what the three functions touch (ADR 0021 §1: column-level,
-- never a "never" column -- none of these is one):
--   legal_documents  SELECT the version list's columns (published_by to resolve the
--                    publisher, body for its byte length, id for RETURNING) and INSERT
--                    the three columns a publication writes. published_at is NOT in the
--                    INSERT list (section 2), and no UPDATE or DELETE is granted: the
--                    table is append-only (00020's trigger binds the owner too).
--   operator_audit_log  SELECT (id) for op_begin_read's INSERT ... RETURNING id, which
--                    the ticket's audit_id needs (handed over by OP-5: m10-platform.md,
--                    OP-5 card correction, item 14 (b)).
--   platform_admins  SELECT (display_name): the version list names the operator who
--                    published. Not a credential column; the digest and the envelope
--                    stay out of the definer's SELECT (ADR 0021 §1).
GRANT SELECT (id, slug, body, published_at, published_by) ON legal_documents TO tappa_opdefiner;
GRANT INSERT (slug, body, published_by) ON legal_documents TO tappa_opdefiner;
GRANT SELECT (id) ON operator_audit_log TO tappa_opdefiner;
GRANT SELECT (display_name) ON platform_admins TO tappa_opdefiner;

-- ===========================================================================
-- 4. THE THREE FUNCTIONS
-- ===========================================================================
-- Every one of them carries 00026's contract (its section 5 header; pinned in the live
-- catalogue for every function tappa_opdefiner owns): SECURITY DEFINER, OWNER
-- tappa_opdefiner, VOLATILE (each writes), SET search_path = pg_catalog, pg_temp with
-- every table `public.`-qualified, REVOKE ALL FROM PUBLIC and EXECUTE to tappa_operator
-- only, the session resolved through op_touch_session (the ONE predicate, §2 i), and a
-- refusal that is one fixed message per function:
--   28000 (invalid_authorization_specification) -- a dead session (op_touch_session's
--         own message) or a refused ticket / refused read;
--   22023 (invalid_parameter_value)              -- an argument the function will not
--         act on, five branches: (1) a read kind or parameter object op_begin_read does
--         not name, (2) a page outside its bounds, (3) a body over the 256 KiB ceiling,
--         (4) a body of [:space:] characters only, (5) a slug or body a constraint of
--         legal_documents refuses. A caller bug, as in op_record_auth_event; each of the
--         five is decided by the argument itself and the schema's constraints, not by a
--         row already in a table, so the code is not a state oracle (ADR 0021 §2 v 7).

-- ---------------------------------------------------------------------------
-- 4.1 op_begin_read -- the shared first phase of the two-phase reads (ADR 0021 §2 v 1)
-- ---------------------------------------------------------------------------
-- Resolves the session, writes the read's audit row and a ticket bound to that row,
-- and returns the RAW ticket. The caller COMMITS, then calls the op_read_* of the same
-- kind in a later transaction (4.2). One statement writes both rows (a data-modifying
-- CTE), and the schema ties them (audit_id NOT NULL REFERENCES, 00026): the commit that
-- makes the ticket usable by phase two is the commit of its audit row.
--
-- PARAMETERS: p_params is the read's parameters as a JSON object, and each read kind
-- names its keys EXACTLY (a missing, an extra or a wrongly typed key is refused). The
-- function rebuilds the object from the TYPED values it extracted (v_bound), so the
-- text that is hashed is the one the op_read_* rebuilds from its own typed arguments;
-- the key order and spacing of the caller's JSON do not reach the hash, and a number
-- whose jsonb text is not a plain positive integer ("10.0", "-1") is refused (jsonb
-- itself writes 1e3 as 1000, measured, so that one is 1000). The closed set
-- of kinds is extended by the migration that adds the next op_read_* (CREATE OR
-- REPLACE), together with operator_read_tickets_kind_check.
--
-- THE TICKET (ADR 0021 §2 v 1, "Karar verilmedi": 256 bits): SHA-256 over three
-- gen_random_uuid() values, i.e. over 366 bits from PostgreSQL's strong random source
-- (pg_strong_random; core, no extension). pgcrypto's gen_random_bytes would be the
-- direct spelling, but the migrations before this one name none of pgcrypto's functions
-- (measured: grep) -- the extension is created by scripts/db-init/01-roles.sql, outside
-- the migrations -- and this file does not add that dependency. 64 lower-case hex
-- characters, returned to the caller and written to no column: the ticket row holds
-- sha256(ticket || v_bound::text) (ADR 0021 §2 v 1, B7).
--
-- LIFETIME: 30 seconds (ADR 0021 §2 v 1: at most 60; OP-5's card correction item 14 (a):
-- strictly below, because created_at's DEFAULT and this expires_at are two clock reads).
-- The two phases are consecutive round trips of one request; 30 s is also the window of
-- ADR 0021 limit 4 (a ticket whose read was rolled back can be read again until it
-- expires).
--
-- THE AUDIT ROW: kind 'read', the session and its operator, target_scope = the read
-- kind, and the page as plain integers (content-free; ADR 0021 §2 v 1). Nothing about
-- the ticket. detail stays '{}'.
--
-- A constraint the write reaches is caught and answered with the fixed refusal (the
-- 00026 section 5 pattern). The checks above it keep the caller's values inside the
-- CHECKs they meet; the ticket's row is the other thing a constraint sees, and an
-- uncaught violation's DETAIL ("Failing row contains (...)") would carry the ticket hash
-- to the caller and the server log -- a value CLAUDE.md §7 and ADR 0021 §3.5 put on the
-- never-log list, raw or hashed.
-- +goose StatementBegin
CREATE FUNCTION public.op_begin_read(p_session text, p_kind text, p_params jsonb)
    RETURNS text
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session     uuid;
    v_admin       uuid;
    v_page_number integer;
    v_page_size   integer;
    v_bound       jsonb;
    v_ticket      text;
    v_rows        bigint;
    v_constraint  text;
    v_state       text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    -- Nested rather than one OR chain: SQL does not promise to evaluate OR's arms in
    -- order, and jsonb_object_keys on a non-object raises instead of answering false.
    IF p_kind IS DISTINCT FROM 'legal_versions' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key))
           IS DISTINCT FROM ARRAY['page_number', 'page_size']
       OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number'
       OR jsonb_typeof(p_params -> 'page_size') IS DISTINCT FROM 'number' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    -- Plain positive integers only: page_number 1..999999999, page_size 1..200 (the
    -- LIMIT ceiling of ADR 0021 §2 iii, and operator_audit_log's own CHECK).
    IF (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$'
       OR (p_params ->> 'page_size') !~ '^[1-9][0-9]{0,2}$' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_page_number := (p_params ->> 'page_number')::integer;
    v_page_size   := (p_params ->> 'page_size')::integer;
    IF v_page_size > 200 THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size);

    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');

    BEGIN
        WITH audit AS (
            INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_scope,
                                                   page_number, page_size)
            VALUES ('read', v_session, v_admin, p_kind, v_page_number, v_page_size)
            RETURNING id
        )
        INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, audit_id,
                                                  expires_at)
        SELECT encode(sha256(convert_to(v_ticket || v_bound::text, 'UTF8')), 'hex'),
               v_session, p_kind, audit.id, clock_timestamp() + interval '30 seconds'
          FROM audit;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_begin_read: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_begin_read: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN v_ticket;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 4.2 op_read_legal_versions -- the first op_read_* (ADR 0021 §2 v 2; ADR 0020 §7)
-- ---------------------------------------------------------------------------
-- Takes the RAW ticket and the read's own typed parameters, recomputes the hash HERE
-- (the stored value is a hash of the ticket, so presenting it as a ticket hashes it
-- again and matches nothing), and consumes the ticket with ONE UPDATE whose WHERE holds
-- the six conditions POSITIVELY (§2 v 2, 4): the hash of (ticket, these parameters),
-- this session, this kind, not yet consumed, not expired by the WALL clock, and its
-- creating transaction COMMITTED -- pg_xact_status(created_xact) = 'committed', which a
-- ticket created in this same transaction (top level or a savepoint: created_xact is the
-- TOP-LEVEL id) does not meet (measured:
-- TestOpReadLegalVersions_ATicketFromThisTransactionIsRefused), and which a NULL (an xid
-- too old to look up) does not meet either. Zero rows = the one refusal. The rows are
-- read only after that UPDATE matched a ticket whose created_xact names a committed
-- transaction; for a ticket op_begin_read wrote, that transaction is the one that wrote
-- it and, in the same statement, the read's audit row (ADR 0020 §5, the M9-08
-- criterion). created_xact comes from its DEFAULT: tappa_opdefiner holds no INSERT on
-- it (00026). The owner can write it, and internal/db's tests use exactly that to reach
-- this predicate without committing rows.
--
-- ⚠️ ADR 0021 limit 4, unchanged: a read transaction that is ROLLED BACK un-consumes the
-- ticket, which can then be read again until it expires (here: 30 s). The re-read writes
-- no new audit row; the committed one names the same session, kind and page.
--
-- THE ROWS: every version of every document, newest first (published_at, then id --
-- the public read path's tie-break), LIMIT = the page size capped at 200 in the body
-- (§2 iii; op_begin_read already refuses a larger one, the cap does not trust that).
-- The columns are a fixed list (§2 ii):
--   version_id, slug, published_at, body_bytes (the length, not the text: a page of 200
--   versions of a 256 KiB document would be 50 MB; the current text is on the public
--   page),
--   publisher_kind     'operator' when published_by is a platform_admins.id, 'legacy'
--                      otherwise -- a row the M7-06 panel wrote (published_by is then a
--                      customer admin_users.id, or NULL) or one the owner wrote by hand.
--                      ADR 0020 §7: the screen shows "tenant admin (legacy)".
--   publisher_admin_id the operator's id for an 'operator' row; NULL for 'legacy'. A
--                      legacy row's admin_users.id is NOT returned: it names a person in a
--                      customer's tenant and the screen does not need it (ADR 0016's
--                      reason for never letting tappa_app read the column).
--   publisher_name     platform_admins.display_name for an 'operator' row; NULL else.
--   is_current         this version is the one the public page serves (the newest of its
--                      slug, by the same ordering as the public read).
-- Telling a legacy row from an operator row by a join is a decision, stated: the two id
-- spaces are separate random v4 uuids (122 random bits each), so a legacy admin_users.id
-- that equals a platform_admins.id is not a case worth a column; the alternative, a
-- publisher-kind column, would need a value for every row written before this migration
-- that nobody can vouch for beyond "not an operator".
-- +goose StatementBegin
CREATE FUNCTION public.op_read_legal_versions(p_session text, p_ticket text,
                                              p_page_number integer, p_page_size integer)
    RETURNS TABLE (version_id uuid, slug text, published_at timestamptz, body_bytes integer,
                   publisher_kind text, publisher_admin_id uuid, publisher_name text,
                   is_current boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('page_number', p_page_number,
                                              'page_size', p_page_size)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'legal_versions'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_legal_versions: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);

    RETURN QUERY
        SELECT d.id,
               d.slug,
               d.published_at,
               octet_length(d.body),
               CASE WHEN a.id IS NULL THEN 'legacy' ELSE 'operator' END,
               a.id,
               a.display_name,
               d.id = (SELECT c.id
                         FROM public.legal_documents AS c
                        WHERE c.slug = d.slug
                        ORDER BY c.published_at DESC, c.id DESC
                        LIMIT 1)
          FROM public.legal_documents AS d
          LEFT JOIN public.platform_admins AS a ON a.id = d.published_by
         ORDER BY d.published_at DESC, d.id DESC
         LIMIT v_limit
        OFFSET (p_page_number - 1)::bigint * v_limit;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_legal_versions(text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_legal_versions(text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_legal_versions(text, text, integer, integer) TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 4.3 op_publish_legal -- the operator's writer of legal_documents (ADR 0020 §7)
-- ---------------------------------------------------------------------------
-- A one-phase write (ADR 0021 §2 v 7): the version and its 'legal_publish' audit row
-- are ONE statement, so either both are there or neither is; RETURNS void, and its
-- answers (void, 28000, 22023) are decided by the session and the arguments, not by
-- what the table held before (a correction is a new row, ADR 0016 §3; "was there a text
-- already" is the version list's business, a two-phase read).
-- published_by is the operator the SESSION resolves to -- there is no actor parameter
-- (§2 i). published_at is the column's DEFAULT, the wall clock (section 2).
-- 256 KiB: ADR 0016 §6's ceiling, 262 144 bytes of the stored text (octet_length, i.e.
-- UTF-8 bytes), refused before anything is written. The HTTP layer bounds the request
-- body as well; this is the database's own copy, which a caller that skipped the
-- handler meets too.
-- The audit detail carries the slug, the new row's id and the byte length -- not the
-- text (legal_documents keeps every version verbatim; the internal/domain/legal
-- PublishedDetail precedent).
-- A body made only of [:space:] characters is refused before anything is written (the
-- body comment says why 00020's CHECK is not enough). Arguments the table refuses (a slug
-- outside 00020's closed set, a NULL slug or body) reach a constraint: caught, one fixed
-- 22023, no DETAIL -- an uncaught "Failing row contains (...)" would echo up to 256 KiB
-- of the body into the server log per refused call.
-- +goose StatementBegin
CREATE FUNCTION public.op_publish_legal(p_session text, p_slug text, p_body text)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_rows       bigint;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF octet_length(p_body) > 262144 THEN
        RAISE EXCEPTION 'op_publish_legal: document refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    -- 00020's CHECK is btrim(body) <> '', and btrim strips SPACES only: a body of
    -- newlines and tabs passes it (measured, 2026-10-03: btrim(E'  \n\t ') <> '' is t)
    -- and would publish a page that is neither the placeholder nor a text -- the case
    -- that CHECK exists for. Refused here for every [:space:] character. (NULL is left
    -- to the column's NOT NULL below.)
    IF p_body !~ '[^[:space:]]' THEN
        RAISE EXCEPTION 'op_publish_legal: document refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        WITH doc AS (
            INSERT INTO public.legal_documents (slug, body, published_by)
            VALUES (p_slug, p_body, v_admin)
            RETURNING id
        )
        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, detail)
        SELECT 'legal_publish', v_session, v_admin,
               jsonb_build_object('slug', p_slug, 'document_id', doc.id,
                                  'bytes', octet_length(p_body))
          FROM doc;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_publish_legal: an argument failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_publish_legal: document refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_publish_legal(text, text, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_publish_legal(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_publish_legal(text, text, text) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00022's and 00026's house style):
--   * it GIVES tappa_app ITS INSERT ON legal_documents BACK -- 00020's column grant
--     exactly -- i.e. it re-opens the write path ADR 0016 §2b calls "no database depth":
--     any tenant's connection can append a legal version again. A security regression
--     by construction; it is 00026's state.
--   * the versions op_publish_legal wrote, and the 'read' / 'legal_publish' audit rows,
--     STAY: both tables are append-only (00020, 00026 -- their triggers bind the owner
--     too), and they are the evidence. So the audit kind CHECK comes back VALIDATED only
--     when no such row exists; when one does it comes back NOT VALID -- enforced for every
--     new row, not re-checked against the old ones (the DO block below). Measured on the
--     development database: with no such row, the schema after Down equalled 00026's in a
--     pg_dump comparison; with such rows, the one difference was NOT VALID.
--   * published_at's DEFAULT goes back to now().
-- The functions go first (they are plpgsql: no recorded dependency on the grants), and
-- the grants they used go after.
DROP FUNCTION IF EXISTS public.op_publish_legal(text, text, text);
DROP FUNCTION IF EXISTS public.op_read_legal_versions(text, text, integer, integer);
DROP FUNCTION IF EXISTS public.op_begin_read(text, text, jsonb);

REVOKE SELECT (display_name) ON platform_admins FROM tappa_opdefiner;
REVOKE SELECT (id) ON operator_audit_log FROM tappa_opdefiner;
REVOKE ALL ON legal_documents FROM tappa_opdefiner;
GRANT INSERT (slug, body, published_by) ON legal_documents TO tappa_app;

ALTER TABLE legal_documents ALTER COLUMN published_at SET DEFAULT now();

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
    CHECK (kind ~ '^[a-z][a-z_]{0,62}$');

ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_read_has_scope;
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind IN ('read', 'legal_publish')) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'login', 'enrollment', 'logout'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'login', 'enrollment', 'logout'));
    END IF;
END
$$;
-- +goose StatementEnd
