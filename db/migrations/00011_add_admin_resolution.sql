-- +goose Up

-- Migration 0011 -- M6-01 PHASE A: the DATA layer of admin authentication.
-- It adds NO table. 00006 already created admin_users / admin_sessions /
-- password_resets and an applied migration is immutable (CLAUDE.md section 6), so
-- everything here is additive: two context-less resolution functions, one index,
-- a column-scoped UPDATE grant and a monotonic-revocation trigger.
--
-- ============================================================================
-- WHY THIS MIGRATION EXISTS AT ALL -- 00006 ASSUMED A PRECONDITION NOBODY SUPPLIES
-- ============================================================================
-- 00006 states: "Employee sessions'taki gibi resolver YOK: admin girisi tenant
-- baglami KURULU iken (login tenant'i biliyor) yapilir." Nothing establishes that
-- context. admin_users.email is unique PER TENANT, `tenants` has no slug/subdomain
-- column, and the product exposes ONE login URL. So a login request carries an
-- email and a password and NO tenant -- exactly the shape ADR 0002 madde 7 exists
-- for: the tenant is the RESULT of the lookup, not an input.
--
-- USER DECISION (2026-08-02): GLOBAL RESOLUTION + TENANT PICKER. One login page,
-- one email; the email resolves GLOBALLY, the password is verified, and if the
-- same email is registered in MORE THAN ONE business the operator picks which one
-- AFTER authenticating. Rationale: the two design-partner tenants (Kebab Factory,
-- Kebab Manufacturing) belong to the SAME person, and a per-tenant login URL would
-- make them log in twice with two identities.
--
-- THE ALTERNATIVE THAT WAS REJECTED, and why it is not a close call: carrying the
-- tenant id in a signed cookie (or a hidden form field, or a subdomain the client
-- asserts). Tenant is the OUTPUT of resolution, never an input -- a middleware that
-- accepts one lets the caller NAME ITS OWN TENANT (M5-03). A signature does not
-- change this: it proves WE minted the value, not WHICH session it belongs to, and
-- the cookie is in the attacker's hands. Resolution stays keyed on a secret the
-- attacker must possess (the session token hash), never on a claim they can make.
--
-- ============================================================================
-- TWO FUNCTIONS, NOT ONE -- MEASURED, NOT ASSUMED
-- ============================================================================
-- Login is not the only context-less moment. EVERY panel request afterwards
-- arrives with just a cookie, and at that instant the tenant is unknown again.
-- Measured on this schema as tappa_app with no context: a direct
-- `SELECT ... FROM admin_sessions WHERE token_hash = $1` returns 0 rows (FORCE RLS,
-- fail-closed). Without a bounded resolver the panel could not authenticate a
-- single request unless the client told us its tenant -- the rejected design above.
-- So there are two:
--   resolve_admin_by_email          -- login: email -> N candidate identities
--   resolve_admin_session_by_token_hash -- every later request: token hash -> <=1 session
--
-- ============================================================================
-- THE FIFTH ADR-0002-madde-7 CONSTRAINT CHANGES SHAPE FOR THE EMAIL LOOKUP
-- ============================================================================
-- The three existing resolvers (tags.uid, sessions.token_hash,
-- employee_invites.code_hash) return AT MOST ONE row because their key is
-- GLOBALLY UNIQUE. resolve_admin_session_by_token_hash is the exact twin of the
-- session one and keeps that property (admin_sessions.token_hash is globally
-- UNIQUE -- 00006).
--
-- resolve_admin_by_email CANNOT. Email is unique only WITHIN a tenant
-- (admin_users_tenant_email_key), by product decision: the same human may own two
-- businesses. So the honest constraint is not "<=1 row" but a MEASURED BOUND:
--
--   at most ONE row per tenant, hence at most count(tenants) rows in total, and
--   never two rows with the same tenant_id.
--
-- That bound is STRUCTURAL, not a promise of the body: it is the partial UNIQUE
-- index (tenant_id, email) WHERE email IS NOT NULL that produces it. NULL emails
-- are excluded by the equality itself (NULL = x is never true), so an admin
-- without an email can never be resolved -- correct: they cannot log in either.
--
-- No LIMIT is imposed HERE, and the reason is the SILENT DROP and nothing else: a
-- LIMIT inside the resolver would cut off a business the operator legitimately
-- belongs to, and a login that fails because the row you needed happened to be
-- row 11 is undebuggable from the outside.
-- WHAT THIS RATIONALE MUST NOT SAY -- and did say, until it was measured -- is
-- "the caller is going to bcrypt-compare each one anyway". That is not a REASON to
-- return N rows; it is the COST of returning them, it is the largest number in
-- this file, and it is named as a limit below (PHASE B OBLIGATION 4) instead of
-- being spent as an excuse. A per-request cap may well turn out to be right -- it
-- just belongs where the bcrypt loop is, decided against a measurement OF that
-- loop, not asserted in a resolver that cannot see it.
--
-- ============================================================================
-- EMAIL ENUMERATION: A NEW ORACLE OPENS HERE. NAMED, BOUNDED, HANDED TO PHASE B.
-- ============================================================================
-- tappa_app may EXECUTE resolve_admin_by_email, so the application can answer "is
-- this email registered, and in how many businesses" for ANY email, WITHOUT a
-- password. That is inherent to global resolution -- authentication must find the
-- identity before it can verify anything -- and it CANNOT be closed in the
-- database. This file therefore states the limit rather than pretending it away.
--
-- PHASE B OBLIGATION (M6-01 auth handler), not optional:
--   1. IDENTICAL response for "no such email", "wrong password" and "disabled
--      admin" -- same status, same body, same wording. The three outcomes are
--      distinguishable HERE (0 rows / hash mismatch / status='disabled'); they must
--      not be distinguishable THERE.
--   2. A DUMMY bcrypt comparison against a fixed dummy hash when the resolver
--      returns 0 rows, so the response TIME does not answer the question the body
--      refuses to. bcrypt is deliberately slow: skipping it is a timing oracle
--      wide enough to measure over the internet.
--   3. Rate limiting on the login endpoint and an audit_log row for failures
--      (M6-01 card). Both are phase B; neither is schema.
--   4. A BOUND ON HOW MANY CANDIDATES ONE REQUEST MAY VERIFY -- the COST limit,
--      measured, and the largest number in this file. This resolver AMPLIFIES:
--      its work is linear in the number of tenants holding the address, and the
--      expensive part is not here. Measured on this schema (500 tenants, 500
--      admin_users rows carrying one victim address, planted inside a transaction
--      and rolled back): the resolver returns 500 rows in ~0.9-1.2 ms warm
--      (2.8 ms cold) -- THE DATABASE IS NOT THE BOTTLENECK. Phase B then
--      bcrypt-compares each row; at cost 10 one comparison is ~60-100 ms, so ONE
--      unauthenticated POST /login buys ~30-50 s of CPU. That is ~500x
--      amplification off a single credential-less request: a DoS bound, not a
--      timing one, and a different failure from obligations 1-3.
--      NOT EXPLOITABLE TODAY, and the reason is worth naming because it expires:
--      no APPLICATION path creates a tenant -- there is no CreateTenant /
--      InsertTenant query in db/queries and no INSERT INTO tenants outside
--      test/fixtures/seed.sql and the test helpers (grepped) -- so an attacker
--      cannot plant the rows. M7-02 changes exactly that. Its sign-up wizard is PUBLIC, and RLS
--      does not stop a tenant from writing ANY email it likes into its OWN
--      admin_users -- so from M7-02 the ATTACKER controls the row count, i.e.
--      controls the bound.
--      OPTIONS ON THE TABLE, to be CHOSEN IN PHASE B AGAINST ITS OWN MEASUREMENT
--      and deliberately NOT chosen here: cap how many candidates a single request
--      will verify · stop at the first match instead of comparing all N · require
--      email verification before a tenant's admin row is resolvable at all
--      (M7-02). Each trades a login failure mode against the DoS bound, and that
--      trade needs the handler's numbers, which do not exist yet.
--      READ THIS TOGETHER WITH OBLIGATION 5 -- the middle option collides with it.
--   5. THE SESSION IS ISSUED ONLY TO THE CANDIDATE WHOSE HASH MATCHED, AND THE
--      PICKER SHOWS ONLY MATCHED CANDIDATES. The heaviest item on this list, and
--      the only one that is a CROSS-TENANT AUTHENTICATION BYPASS (section 4.5)
--      rather than an enumeration signal or a DoS bound. It was added last because
--      it was MISSED: obligations 1-4 are all about the shape of the resolver's
--      answer, and this one is about the LINK between two different steps -- WHICH
--      candidate's hash verified, and WHICH identity got a session.
--      WHY THE DATABASE CANNOT ENFORCE IT. resolve_admin_by_email returns a SET.
--      CreateAdminSession never sees a password: all it demands is that
--      (admin_user_id, tenant_id) be internally consistent and the admin be active
--      -- and a picker hands back exactly such a pair, made of REAL rows, so the
--      INSERT ... SELECT guard, the explicit tenant predicate and the RLS WITH
--      CHECK are all satisfied. Every protection in this file is happy; the bind
--      that is missing does not exist anywhere in SQL.
--      THE ATTACK, live from M7-02 on: the attacker signs up their own tenant with
--      the PUBLIC wizard and writes the VICTIM'S EMAIL, with a password THEY know,
--      into their OWN admin_users -- RLS permits it, it is their tenant. Login with
--      that address resolves TWO candidates; phase B verifies the password and it
--      matches the ATTACKER'S row; the picker then asks "which business?" and the
--      attacker chooses the VICTIM'S. Measured on this schema:
--        -- C1: only AttackerCo's hash verified; the picker offers VictimCo
--        INSERT ... SELECT ... WHERE a.id='aaaa...0001'
--                                AND a.tenant_id='1111...1111'   -- VICTIM
--                                AND a.status='active';
--          -> INSERT 0 1        (a session stamped with the VICTIM's tenant)
--        -- C2: and every following request passes too
--          -> UPDATE 1 | role=owner | full_name=Victim Owner
--      ⚠️ TENSION WITH OBLIGATION 4 -- STATED BECAUSE THE TWO PULL OPPOSITE WAYS
--      AND NOTHING SAID SO. "Stop at the first match" is a legitimate answer to the
--      bcrypt DoS, but implemented as "stop at the first match AND let the picker
--      list every candidate" it IS the bypass above, exactly. The two obligations
--      must be read together: whatever bounds the work, THE SET THE PICKER OFFERS
--      MUST NEVER BE WIDER THAN THE SET WHOSE HASH ACTUALLY VERIFIED.
-- Nothing in this migration can enforce any of the five.
--
-- KIRMIZI CIZGILER touched:
--   section 4.5  Tenant isolation. The two functions are the ONLY admin lookups
--                without a tenant filter, and they are contained exactly as ADR
--                0002 madde 7 requires (definer tappa_resolver, fixed search_path,
--                schema-qualified body, column-level SELECT, EXECUTE to tappa_app
--                alone, no PUBLIC EXECUTE, fixed column list).
--   section 4.7  A TOKEN is never stored or returned -- only its hash, and the
--                hash is an INPUT here, never an output. password_hash IS
--                returned (see below): it is the value authentication compares
--                against and there is no context in which to read it otherwise.
--   section 4.6  No row is deleted. The revocation trail is now MONOTONIC in the
--                database instead of by file discipline (see the trigger).


-- --- Index for the global email lookup ----------------------------------------
-- 00006's only email index is (tenant_id, email) WHERE email IS NOT NULL -- a
-- TENANT-FIRST index, useless as a driver for a lookup that has no tenant. This
-- one supports the login path. It is deliberately NOT unique: two tenants may hold
-- the same email, which is the whole point of the tenant picker.
--
-- Partial (email IS NOT NULL) for the same reason as 00006's: an admin without an
-- email is not addressable by email. An equality predicate implies NOT NULL, so
-- the planner can still use it for the resolver's WHERE.
--
-- It does NOT weaken R5: admin_users_tenant_idx (tenant_id) is untouched and still
-- satisfies the "tenant_id-first index" element of the five.
--
-- LIMIT, STATED BECAUSE IT CANNOT BE PINNED: this index is a PERFORMANCE property
-- and NO test in the suite fails without it. Measured: dropped the index, ran the
-- whole internal/db package (-race -count=1, real Postgres) -- GREEN; recreated it
-- and verified the definition is identical. A mutation that removes this index is
-- therefore invisible to CI, which is why the guarantee has to live in a comment
-- instead of in a test.
-- What WAS measured about it, honestly scoped: on the working database as it
-- stands (3026 admin_users rows) the planner CHOOSES this index at DEFAULT
-- settings -- `Index Scan using admin_users_email_idx, Index Cond: (email = ...)`
-- -- and does so under enable_seqscan=off as well. On a small or freshly seeded
-- table a sequential scan is cheaper and the planner will take it; that is correct
-- behaviour, not evidence the index is dead. The claim is "usable and chosen at
-- realistic row counts", never "always used".
CREATE INDEX admin_users_email_idx ON admin_users (email) WHERE email IS NOT NULL;


-- --- Column-level SELECT for the bounded definer -------------------------------
-- tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER, no default privileges) may now
-- read exactly the columns the two resolutions need -- nothing else on these
-- tables, and still nothing at all on any other table.
--
-- admin_users: email is needed because the body FILTERS on it; id/tenant_id/
-- password_hash/status because the body RETURNS them (justified one by one at the
-- function). full_name, role, created_at and last_login_at are NOT granted.
GRANT SELECT (id, tenant_id, email, password_hash, status)
    ON admin_users TO tappa_resolver;

-- admin_sessions: token_hash to filter, the rest to return. created_at and
-- last_used_at are NOT granted -- a resolution does not need them.
GRANT SELECT (id, tenant_id, admin_user_id, token_hash, revoked_at)
    ON admin_sessions TO tappa_resolver;


-- --- resolve_admin_by_email (LOGIN) --------------------------------------------
-- Returns EVERY admin identity registered under this email, across tenants.
--
-- COLUMN LIST -- each one is here because authentication cannot proceed without it:
--   id            the session is issued FOR this admin; without it phase B would
--                 have to look the row up again by email once the context exists,
--                 re-exposing the email for no gain.
--   tenant_id     THE POINT of the whole function: it is what the caller sets as
--                 app.tenant_id, after which every other read runs under RLS.
--   password_hash the value the password is compared against. This is the ONLY
--                 reason the lookup must be context-less: there is no tenant
--                 context to read it in until authentication succeeds.
--   status        AUTHENTICATION input, not decoration. 00003's session resolver
--                 does NOT return employees.status, and state.md carries that as an
--                 inherited liability ("every surface outside the tap path must
--                 check employees.status ITSELF") -- a rule someone will forget.
--                 Returning it here makes "a disabled admin cannot authenticate"
--                 decidable at the boundary where authentication happens. It is
--                 belt-and-braces, not the only belt: CreateAdminSession
--                 (db/queries/admins.sql) refuses to issue a session for a
--                 non-active admin in SQL, so forgetting the check still fails
--                 closed.
--
-- DELIBERATELY NOT RETURNED, and why each is genuinely unnecessary:
--   role          AUTHORIZATION input, never read before a tenant context exists.
--                 The agent brief listed it as a minimum column; it is omitted
--                 after measuring the call flow, and the measurement is this: the
--                 tenant PICKER has to show the BUSINESS NAME, which lives in
--                 `tenants` and which this function deliberately will not touch (a
--                 second table on the bypass surface, and a pre-auth name is itself
--                 an enumeration signal). So phase B must do one tenant-scoped read
--                 per candidate anyway -- and `role` rides along in that read for
--                 free (GetAdminForTenantChoice). Returning it here would widen the
--                 pre-auth surface for a value nothing pre-auth reads, which is
--                 exactly what ADR 0002 madde 7 constraint (iii) forbids.
--   full_name     display only; in-context (GetAdminByID).
--   email         it is the INPUT. Echoing it back invites logging it and buys
--                 nothing.
--   created_at,
--   last_login_at audit/display; in-context.
--
-- STATUS IS RETURNED BUT NOT FILTERED, exactly like sessions.revoked_at (00003) and
-- employee_invites.used_at (00009): the data layer carries the truth and the domain
-- decides. A resolver that hid disabled admins would report "unknown email" for an
-- email that exists, and phase B could not tell a disabled account from a typo --
-- which is how a login attempt on a disabled account becomes unattributable
-- (section 4.6).
--
-- ORDER BY tenant_id IS DETERMINISM, AND IT IS UNPINNABLE -- the same limit as the
-- index above, stated for the same reason. Measured: replaced the live function
-- with an otherwise byte-identical body carrying NO ORDER BY, ran the whole
-- internal/db package (-race -count=1) -- GREEN; then restored the shipped body
-- and re-verified owner (tappa_resolver), ACL and proconfig unchanged. Nothing
-- mechanical defends this ordering. It is kept because an unordered set-returning
-- function makes the candidate list -- and with it any log line, any picker order,
-- any future test that compares slices -- depend on physical row order, which
-- changes under VACUUM and under plan changes. The PICKER's display order is the
-- tenant NAME, read in-context; this ordering is for the machine, not the
-- operator.
--
-- THE citext TRAP -- MEASURED, AND THE REASON FOR THE ODD-LOOKING OPERATOR.
-- admin_users.email is citext, whose `=` operator lives in the `public` schema.
-- This function fixes search_path to `pg_catalog, pg_temp` (injection defence), so
-- that operator is NOT VISIBLE -- and Postgres does not error: it falls back to
-- text = text through citext's IMPLICIT cast to text, making the lookup CASE
-- SENSITIVE. Measured on this database, plain `a.email = p_email` against a stored
-- 'Owner@Example.Test': exact case 1 row, lowercase 0 rows, uppercase 0 rows. With
-- OPERATOR(public.=) all three return 1. Two things would have broken silently:
-- an operator typing their address in lowercase could not log in, and the
-- function's equality would no longer be the SAME equality as the UNIQUE index
-- that underwrites the at-most-one-row-per-tenant bound above.
--
-- THE TRAP IS A search_path PROPERTY, NOT A SECURITY DEFINER ONE. Measured, and
-- written down so nobody reads the paragraph above as "only definer functions are
-- affected". The same probe with NO function involved at all: a plain
-- `SELECT ... FROM public.admin_users a WHERE a.email = '<literal>'` run under
-- `SET LOCAL search_path = pg_catalog, pg_temp` finds 1 row for the exact
-- spelling and 0 for lowercase and 0 for uppercase; the byte-identical statement
-- under the default `"$user", public` finds 1/1/1, and adding OPERATOR(public.=)
-- makes the pinned case find 1 again. ANYTHING that pins search_path away from
-- public -- a definer function, a trigger function, a session that hardens itself,
-- a future migration -- degrades citext equality the same silent way.
--
-- THE CLASS IS CLOSED TODAY, which is exactly why it needs writing down: it will
-- not stay closed on its own. The schema has precisely TWO citext columns --
-- admin_users.email (00006) and employees.email (00003); measured, there are no
-- others. And exactly ONE query in the repo filters on a citext column: this
-- function (grep for an equality/ILIKE against an email column across
-- db/queries and db/migrations -- 00011 is the only hit). employees.email is
-- written and displayed but never used as a lookup key. So the FIRST person to
-- write `WHERE email = ...` over employees -- an invite-by-email flow, a duplicate
-- check, an admin-side search -- inherits this trap, in whatever context happens
-- to pin search_path. Qualify the operator, or measure all three spellings.
-- +goose StatementBegin
CREATE FUNCTION resolve_admin_by_email(p_email citext)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        password_hash text,
        status        text
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    -- KRITIK (search_path injection): search_path is pinned. public is NOT on the
    -- path, so the table is explicitly qualified public.admin_users and the citext
    -- equality is explicitly qualified OPERATOR(public.=). pg_temp last (Postgres
    -- recommendation).
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- N rows, bounded by the partial UNIQUE (tenant_id, email): at most one per
        -- tenant, never two of the same tenant. Fixed column list, no SELECT *.
        -- ORDER BY tenant_id only for a deterministic result; the picker's display
        -- order is the tenant NAME, which is read in-context (see the comment above).
        SELECT a.id, a.tenant_id, a.password_hash, a.status
        FROM public.admin_users AS a
        WHERE a.email OPERATOR(public.=) p_email
        ORDER BY a.tenant_id;
    $$;
-- +goose StatementEnd

-- EN-AZ-AYRICALIK: owner tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER). A
-- SECURITY DEFINER body runs with the OWNER's rights; a superuser owner would
-- bypass RLS database-wide -- the general bypass ADR 0002 madde 7 forbids.
ALTER FUNCTION resolve_admin_by_email(citext) OWNER TO tappa_resolver;

-- KRITIK: functions get PUBLIC EXECUTE by default. Revoke it all, then grant to
-- the single legitimate caller.
REVOKE ALL ON FUNCTION resolve_admin_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_admin_by_email(citext) TO tappa_app;


-- --- resolve_admin_session_by_token_hash (EVERY PANEL REQUEST) -----------------
-- The exact twin of 00003's resolve_session_by_token_hash, over the SEPARATE admin
-- table: an admin cookie resolves here and NOWHERE ELSE, an employee cookie
-- resolves in 00003 and nowhere else. The separation is structural (two tables, two
-- functions), not a flag on a shared row -- M1-11's whole point.
--
-- <=1 row, structurally: admin_sessions.token_hash is GLOBALLY UNIQUE (00006).
--
-- revoked_at is RETURNED but NOT FILTERED (00003's principle): the caller must be
-- able to tell "revoked session" from "unknown token" -- the first is an event
-- worth auditing, the second is noise.
--
-- admin_users.status is deliberately NOT joined in here. The very next statement in
-- the flow -- TouchAdminSession, tenant-scoped, RLS-covered -- refuses to refresh a
-- session whose admin is not active, so a disabled admin fails closed on the
-- authoritative write instead of on a fact this function could have carried. That
-- keeps the bypass surface at ONE table for this function.
-- +goose StatementBegin
CREATE FUNCTION resolve_admin_session_by_token_hash(p_token_hash text)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        admin_user_id uuid,
        revoked_at    timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- <=1 row: token_hash is globally UNIQUE. Fixed column list, no SELECT *.
        -- token_hash is the INPUT and is never returned (section 4.7); created_at
        -- and last_used_at are not needed to resolve.
        SELECT s.id, s.tenant_id, s.admin_user_id, s.revoked_at
        FROM public.admin_sessions AS s
        WHERE s.token_hash = p_token_hash;
    $$;
-- +goose StatementEnd

ALTER FUNCTION resolve_admin_session_by_token_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION resolve_admin_session_by_token_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_admin_session_by_token_hash(text) TO tappa_app;


-- --- admin_sessions: column-scoped UPDATE --------------------------------------
-- 00006 grants tappa_app TABLE-WIDE UPDATE. Measured as tappa_app inside a tenant
-- context, all three of these succeeded (UPDATE 1 each):
--   (a) UPDATE admin_sessions SET revoked_at = NULL           -- un-revoke
--   (b) UPDATE admin_sessions SET admin_user_id = <owner id>  -- a MANAGER's live
--       session repointed at the OWNER: privilege escalation with one statement,
--       same tenant, RLS satisfied.
--   (c) UPDATE admin_sessions SET token_hash = <hash I know>  -- session hijack:
--       bind an existing session row to a token of the attacker's choosing.
-- (b) and (c) were unnamed anywhere in this repo; the sessions.sql note only ever
-- admitted to (a). A column grant closes both at the PRIVILEGE level.
--
-- This is the 00009 (employee_invites) precedent applied where it fits: a column
-- grant is worth writing when the table has FEW writable columns. admin_sessions
-- has exactly two -- last_used_at (refresh) and revoked_at (revocation). Contrast
-- admin_users, whose grant is deliberately left table-wide: nearly every column
-- there is legitimately writable (password_hash on reset, status on disable,
-- full_name/email/role in panel management, last_login_at on login), so a column
-- grant would merely enumerate the table. tenant_id is already unwritable in
-- practice -- the RLS WITH CHECK refuses any value other than the context.
--
-- ⚠️ AND THEREFORE, SAID PLAINLY RATHER THAN LEFT TO INFERENCE: two of the three
-- effects closed above stay reachable ONE TABLE OVER. Measured as tappa_app in its
-- own tenant context: SET status='active' -> UPDATE 1 (the disable kill switch,
-- undone), SET role='owner' -> UPDATE 1 (the SAME escalation), and
-- SET password_hash=<mine> -> UPDATE 1, which is STRONGER than anything closed here
-- because it survives revocation. What this migration closes is a PATH on
-- admin_sessions, not a CAPABILITY -- and no wording in this repo may say otherwise.
-- WHOSE DEBT: M6-05 (panel-side admin management) and M7-04 (password reset), which
-- write the legitimate UPDATEs and are the first code with the context to decide
-- whether admin_users needs column grants, a role guard or an audit trigger.
-- Until then the protection is "no query in this repo does it" -- discipline, the
-- position sessions.sql is in.
--
-- DIKKAT (M1-04 lesson): db-init's ALTER DEFAULT PRIVILEGES grants the four DML
-- privileges on every new table, and 00006's GRANT added table-wide UPDATE on top.
-- Removing a privilege therefore REQUIRES an explicit REVOKE; narrowing a GRANT
-- does nothing. So: revoke the table-wide UPDATE first, then grant the two columns.
-- DELETE stays revoked (00006, section 4.6) and is not touched here.
REVOKE UPDATE ON admin_sessions FROM tappa_app;
GRANT UPDATE (last_used_at, revoked_at) ON admin_sessions TO tappa_app;


-- --- Monotonic revocation (the structural half) --------------------------------
-- A column grant constrains WHICH column may be written, never WHAT VALUE it may
-- take -- so `SET revoked_at = NULL` is still privilege-legal, i.e. (a) above is
-- NOT closed by the grant. state.md carries this exact gap for sessions.revoked_at
-- as an inherited finding ("the structural fix is a TRIGGER and needs a NEW
-- migration"). This is that migration, for the admin table.
--
-- Why it matters more here than anywhere: revocation is an instant kill switch
-- (lost laptop, dismissed manager). If revoked_at can go back to NULL, "signed out
-- everywhere" is a suggestion, and the audit answer to "when did this session die"
-- can be rewritten.
--
-- CORRECTION, because an earlier wording here called revocation "the panel's ONLY
-- instant kill switch" and that is FALSE, in a direction that matters: the STRONGER
-- switch is admin_users.status = 'disabled'. TouchAdminSession joins admin_users and
-- refuses a non-active admin, so ONE row change kills EVERY session that admin
-- holds, present and future, without touching admin_sessions at all -- whereas
-- revoked_at kills one session per row. And the stronger switch has NO monotonicity
-- protection whatsoever: admin_users keeps a table-wide UPDATE grant, so
-- SET status='active' silently restores every one of those sessions (UPDATE 1,
-- measured). The trigger below hardens the WEAKER of the two. That is not a reason
-- to skip it -- revoked_at is also the audit record, and rewriting a record is a
-- different harm from flipping a flag -- but it is a reason not to read this file as
-- "the kill switch is now monotonic". It is not; see the admin_users note above.
--
-- The trigger binds tappa_owner (the migration superuser) too, which no REVOKE can
-- -- the same belt 00005 puts over transactions/audit_log. A superuser can still
-- DISABLE the trigger; this is defense-in-depth, not an absolute (00005's note).
--
-- The function is table-agnostic on purpose: the WHEN clause below carries the whole
-- condition and the body only RAISEs, so a future migration can reuse it verbatim to
-- close the same hole on `sessions` (00003) without writing a second function. That
-- fix is a DELIBERATE NON-GOAL here: this migration is admin auth, and changing the
-- employee session table would reach into M5 surfaces that are not under test in
-- this task. WHOEVER ACCEPTS THIS INVITATION MUST READ "BOUNDARY 2" BELOW FIRST:
-- attaching this trigger to a table whose revocation query lacks COALESCE or an
-- `IS NULL` guard converts a concurrent double-revoke into a rolled-back
-- transaction, i.e. into a session that survives a sign-out. Fail-OPEN.
--
-- SECURITY INVOKER (default): the body reads no data, so DEFINER would be a
-- privilege surface for nothing. search_path pinned (injection).
--
-- PUBLIC EXECUTE IS LEFT IN PLACE HERE, unlike the two resolvers above which are
-- explicitly REVOKEd from PUBLIC. That is an INCONSISTENCY, so it is measured
-- rather than left looking like an oversight. Functions get PUBLIC EXECUTE by
-- default and this one keeps it: pg_proc.proacl IS NULL and
-- has_function_privilege('public', ...) = true for it, against FALSE for both
-- resolve_admin_* functions. It is inert three ways, each measured as tappa_app:
--   * calling it directly -> ERROR "trigger functions can only be called as
--     triggers" (it returns `trigger`; PL/pgSQL refuses at compile time);
--   * attaching it to the real table -> CREATE TRIGGER ... ON admin_sessions ->
--     ERROR "permission denied for table admin_sessions" (CREATE TRIGGER requires
--     table OWNERSHIP, which tappa_app does not have and cannot acquire);
--   * attaching it to a table of its own -> CREATE TABLE -> ERROR "permission
--     denied for schema public" -- tappa_app cannot create a table to attach it to.
-- The body also reads nothing and only RAISEs, so even a hypothetical successful
-- call discloses nothing. A REVOKE would still be tidier and costs nothing at
-- runtime; it is deliberately NOT added in this pass, because 00011 is APPLIED on
-- the working database and editing the DDL of an applied migration is what
-- CLAUDE.md section 6 forbids -- the file and the database would diverge. If the
-- tidiness is wanted it is a one-line follow-up migration, not an edit here.
-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_revocation_reset()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'revocation is monotonic on %: revoked_at may go NULL -> a timestamp, never back and never to another instant',
            TG_TABLE_NAME
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd

-- WHEN carries the entire condition, so an ordinary last_used_at refresh never even
-- enters the function. IS DISTINCT FROM (not <>) so a NULL new value is caught.
-- Re-stamping a DIFFERENT instant is refused as well as un-revoking: the stamp is
-- the audit answer to "when did this session die" and rewriting it is a quiet edit.
-- Idempotent revocation still works: RevokeAdminSession writes
-- COALESCE(revoked_at, now()), which on an already-revoked row produces the SAME
-- value -> IS DISTINCT FROM is false -> the trigger does not fire.
--
-- BOUNDARY 1 -- THE TRIGGER FREEZES A WRONG FIRST STAMP TOO. It constrains the
-- TRANSITION, never the VALUE, so the first write is unchecked and then permanent.
-- Measured: UPDATE admin_sessions SET revoked_at = '1970-01-01' -> UPDATE 1, and the
-- correction that follows -> ERROR "revocation is monotonic". There is no
-- CHECK (revoked_at IS NULL OR revoked_at >= created_at) on this table, so nothing
-- rejects a stamp from before the session existed or from the future. No query in
-- db/queries writes anything but COALESCE(revoked_at, now()) / now(), so this is a
-- boundary rather than a live hole -- but it is the one way the audit answer can
-- still be wrong, and it is wrong PERMANENTLY, which is worse than editable.
--
-- BOUNDARY 2 -- A FUTURE REVOCATION QUERY CAN FAIL **OPEN** UNDER CONCURRENCY, and
-- this must reach whoever accepts the reuse invitation above (the same trigger on
-- `sessions`, or M7-04's password-reset revocation). The two queries SHIPPED here
-- are safe, and that was measured rather than assumed: with T1 holding the row lock,
-- T2's COALESCE(revoked_at, now()) is RE-EVALUATED against T1's committed row, so
-- T2 writes the same value, IS DISTINCT FROM is false and the trigger never fires.
-- The safety comes from COALESCE and from RevokeAdminSessionsForAdmin's
-- `revoked_at IS NULL` predicate -- NOT from the trigger. A query written as a bare
-- SET revoked_at = now() with no guard hits the trigger the moment two requests race
-- (or a sign-out overlaps a "sign out everywhere"), and the trigger raises an
-- EXCEPTION: the whole transaction rolls back, so "sign out everywhere" REPORTS
-- FAILURE and the session KEEPS LIVING. Fail-OPEN, from a mechanism added to make
-- revocation stronger. Rule for the next author: every write to revoked_at carries
-- COALESCE(revoked_at, ...) or a `revoked_at IS NULL` predicate, or both.
CREATE TRIGGER admin_sessions_revocation_monotonic
    BEFORE UPDATE ON admin_sessions
    FOR EACH ROW
    WHEN (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
    EXECUTE FUNCTION tappa_forbid_revocation_reset();


-- NOTE (out of scope, deliberate): the back-FKs 00005 predicted for
-- transactions.entered_by / transaction_reviews.reviewer_id / audit_log.actor_id ->
-- admin_users are still NOT added (state.md carries them as M6 debt). They belong
-- to the review/manual-entry wiring (M6-04), not to authentication, and
-- reviewer_id's fixture would need rewriting with them. Flagged, not silently
-- skipped.

-- +goose Down

-- Reverse order: trigger, then its function, then the two resolvers, then the
-- grants, then the index. The tappa_resolver ROLE belongs to db-init and is NOT
-- dropped here.
DROP TRIGGER IF EXISTS admin_sessions_revocation_monotonic ON admin_sessions;
DROP FUNCTION IF EXISTS tappa_forbid_revocation_reset();
DROP FUNCTION IF EXISTS resolve_admin_session_by_token_hash(text);
DROP FUNCTION IF EXISTS resolve_admin_by_email(citext);

-- Revoking a table-level privilege also revokes the column-level privileges on
-- that table (verified after running this Down: has_column_privilege is false for
-- every admin_users/admin_sessions column tappa_resolver held).
REVOKE ALL ON admin_users FROM tappa_resolver;
REVOKE ALL ON admin_sessions FROM tappa_resolver;

-- Restore 00006's grant exactly: table-wide UPDATE back, DELETE still revoked.
REVOKE UPDATE ON admin_sessions FROM tappa_app;
GRANT SELECT, INSERT, UPDATE ON admin_sessions TO tappa_app;
REVOKE DELETE ON admin_sessions FROM tappa_app;

DROP INDEX IF EXISTS admin_users_email_idx;
