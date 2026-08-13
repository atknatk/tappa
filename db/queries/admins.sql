-- admins.sql -- the TENANT-SCOPED half of panel authentication (M6-01 phase A):
-- issuing, refreshing and revoking the ADMIN session, plus the two in-context
-- reads the login flow needs once the tenant is known.
--
-- Every query below carries an EXPLICIT tenant_id predicate (CLAUDE.md section
-- 4.5, belt + braces on top of RLS) and is meant to run inside
-- db.(*DB).WithTenant, i.e. with app.tenant_id established. The TWO admin lookups
-- that carry no tenant scope live elsewhere on purpose -- db/queries/resolve.sql,
-- ADR 0002 madde 7 -- because there the tenant is the RESULT of the lookup, not an
-- input. They are deliberately NOT duplicated here.
--
-- SEPARATE FROM THE EMPLOYEE SESSION, STRUCTURALLY (M1-11). Nothing in this file
-- touches `sessions`, `employees` or `employee_invites`, and nothing in
-- db/queries/sessions.sql touches `admin_*`. An admin cookie resolves through
-- resolve_admin_session_by_token_hash over admin_sessions; an employee cookie
-- through resolve_session_by_token_hash over sessions. Two tables, two functions,
-- two column sets -- a token from one is not a row in the other, so the isolation
-- is a property of the schema and not of a flag anyone can mis-set.
--
-- KIRMIZI CIZGI (section 4.7): the raw session token appears nowhere in this file
-- and is not stored. Only token_hash is written or matched, and NO query below
-- RETURNS it. The precise, grep-checkable form of that claim -- because a looser
-- one was written here first and a grep refuted it: `token_hash` appears in NO
-- RETURNING list in this file, and in exactly ONE select-list, the
-- `SELECT a.tenant_id, a.id, @token_hash::text` of CreateAdminSession, where it is
-- the VALUE BEING INSERTED by an INSERT ... SELECT and not a value handed back.
-- The property that actually matters is measured on the generated code: no *Row
-- struct in internal/store/admins.sql.go carries a TokenHash field -- the only
-- TokenHash there is CreateAdminSessionParams', i.e. an INPUT. So a row read
-- cannot leak the hash into a log, and the guarantee survives a regeneration
-- rather than depending on how this comment is worded. password_hash is likewise
-- never selected by any query in this file: the only place it is read is the
-- context-less login resolver, which needs it to verify a password and returns it
-- to exactly one caller.
--
-- NO DELETE QUERIES BY DESIGN: 00006 REVOKEs DELETE on both admin tables, so no
-- application code path can destroy an admin or a session row (section 4.6).
-- Revocation is a revoked_at stamp; disabling an admin is status='disabled'.
--
-- WHAT THE DATABASE NOW ENFORCES THAT sessions.sql ONLY ASKED FOR. The employee
-- file has to say "the protection against un-revoking is that NO QUERY IN THIS
-- FILE DOES IT". For admin_sessions that is no longer discipline: 00011 revokes
-- table-wide UPDATE (leaving column-level UPDATE on last_used_at and revoked_at
-- only -- so token_hash and admin_user_id cannot be rewritten at all) and adds a
-- BEFORE UPDATE trigger that refuses any change to a revoked_at that is already
-- set. Measured: un-revoke, session-hijack-by-UPDATE and
-- repoint-my-session-at-the-owner all succeeded before 00011 and all fail after.
-- The remaining discipline is smaller but real: a NULL -> now() stamp is still
-- free-form, so a query could stamp a future or past instant. No query here does.
--
-- ⚠️ WHAT CLOSED IS THE PATH, NOT THE CAPABILITY -- the sentence above used to be
-- read the other way and the correction is worth spelling out, because it changes
-- who has to be careful.
--
-- ✅ AND admin_users NO LONGER KEEPS A TABLE-WIDE UPDATE GRANT. This paragraph said it
-- did, "deliberately (00011 argues it, correctly: nearly every column there is
-- legitimately writable, so a column grant would merely enumerate the table)" --
-- migration 00017 took that back and refuted the reason. The list 00011 called an
-- enumeration IS the grant now (full_name, email, password_hash, role, status,
-- last_login_at), and the three columns it OMITS are the point: `id` and `tenant_id`
-- (identity) and `created_at` (the ordering 00017's whole security argument rests on
-- -- a row whose created_at can be rewritten can be moved in front of an existing
-- customer, which is the account lockout that file exists to close). Measured after
-- 00017: relacl `tappa_app=ar`, and has_column_privilege for UPDATE on those three is
-- false.
--
-- THE EFFECTS BELOW WERE MEASURED BEFORE THAT NARROWING AND TWO OF THEM SURVIVE IT,
-- which is why they are still here: `status` and `password_hash` and `role` are all in
-- the new grant, because a panel that manages admins has to write them. So the
-- reachability claim is unchanged -- only the columns it can reach through are.
-- Measured live as tappa_app, inside its OWN tenant context:
--   UPDATE admin_users SET status = 'active'        WHERE ... -> UPDATE 1
--       the disable kill switch, undone.
--   UPDATE admin_users SET role = 'owner'           WHERE ... -> UPDATE 1
--       the SAME privilege escalation the column grant closed on admin_sessions.
--   UPDATE admin_users SET password_hash = '<mine>' WHERE ... -> UPDATE 1
--       STRONGER than what was closed: it survives revocation entirely.
-- So the guarantee this file may claim is narrow and exact: no application code
-- path can rewrite a SESSION's identity or resurrect its revocation. It may NOT
-- claim that the underlying abilities are gone -- they are reachable through
-- admin_users by any SQL that runs as tappa_app.
-- WHOSE DEBT: M6-05 (panel-side admin management writes the legitimate UPDATEs and
-- must scope them per column and per role) and M7-04 (password reset). Whoever
-- writes those decides whether admin_users deserves its own column grants, a role
-- guard, or an audit trigger; today the answer is "no query in this repo does it",
-- which is exactly the discipline-not-structure position sessions.sql is in.

-- name: CreateAdminSession :one
-- Issues a panel session. Written as INSERT ... SELECT so the admin's existence,
-- tenancy and ACTIVE status are conditions of the INSERT ITSELF: a disabled admin
-- yields 0 rows and sqlc returns pgx.ErrNoRows. That makes "a disabled admin never
-- gets a session" a property of the DATABASE rather than of the handler
-- remembering to check status -- the trap state.md records for employees (the
-- session resolver does not return employees.status, so every surface must check
-- it itself). The login resolver DOES return admin status, so phase B can refuse
-- earlier and with a uniform message; this is the belt underneath that.
--
-- tenant_id is taken from the ADMIN ROW, not from the caller's parameter, so the
-- session cannot be stamped with a tenant the admin does not belong to. The
-- @tenant_id parameter is still spelled out as a predicate (section 4.5 belt) and
-- the RLS WITH CHECK independently refuses a value that disagrees with the
-- transaction context.
--
-- token_hash is the HMAC of a token this query never sees; it is written, never
-- returned.
INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash)
SELECT a.tenant_id, a.id, @token_hash::text
FROM admin_users a
WHERE a.id = @admin_user_id
  AND a.tenant_id = @tenant_id
  AND a.status = 'active'
RETURNING id, tenant_id, admin_user_id, created_at, last_used_at, revoked_at;

-- name: TouchAdminSession :one
-- THE per-request authority for the panel, and the reason the session resolver
-- does not need to carry the admin's status. It does three things in one
-- statement, so no code path can perform two of them and skip the third:
--   * proves the session is still live (revoked_at IS NULL);
--   * proves the admin behind it is still ACTIVE (join + status test) -- a manager
--     disabled thirty seconds ago stops passing here, without any revocation
--     having been issued;
--   * refreshes last_used_at.
-- Returns the identity the request should act as, including `role`, so the caller
-- has what the policy engine's actor:role context key needs without a second
-- query.
--
-- It fails CLOSED: revoked session, disabled admin and unknown id all produce 0
-- rows and pgx.ErrNoRows. They are deliberately indistinguishable to the caller --
-- all three mean "you are not authenticated, go to the login page" -- and the
-- distinction, when it matters for auditing, comes from the resolver, which
-- carries revoked_at as a fact.
--
-- last_used_at is the only column written, which is all the 00011 column grant
-- permits.
UPDATE admin_sessions s
SET last_used_at = now()
FROM admin_users a
WHERE s.id = @id
  AND s.tenant_id = @tenant_id
  AND s.revoked_at IS NULL
  AND a.id = s.admin_user_id
  AND a.tenant_id = s.tenant_id
  AND a.status = 'active'
RETURNING s.id, s.last_used_at, a.id AS admin_user_id, a.role, a.full_name;

-- name: RevokeAdminSession :one
-- Revokes one panel session (sign out). COALESCE keeps the FIRST revocation
-- timestamp: a repeated revoke is idempotent AND does not rewrite when the session
-- actually died (audit truth). It is also what keeps the 00011 monotonicity
-- trigger quiet on a repeat -- the new value equals the old one, so nothing
-- changes and the trigger does not fire.
--
-- The row is matched regardless of its revocation state, so pgx.ErrNoRows means
-- "no such session in this tenant", which is a genuinely different outcome from
-- "already revoked" and callers must be able to tell them apart.
--
-- ⚠️ THE COALESCE IS LOAD-BEARING UNDER CONCURRENCY, NOT JUST TIDY -- a note for
-- whoever writes the NEXT revocation query (M7-04's password reset, or the same
-- trigger reused on `sessions`). Measured: with T1 holding the row lock, T2's
-- COALESCE is RE-EVALUATED against T1's committed row, so T2 writes the identical
-- value, the trigger's IS DISTINCT FROM is false and nothing fires. Both queries in
-- this file are safe for that reason -- COALESCE here, `revoked_at IS NULL` in
-- RevokeAdminSessionsForAdmin -- and NOT because the trigger tolerates races.
-- A future `SET revoked_at = now()` with neither guard RAISES on the second
-- concurrent writer; the exception rolls back the whole transaction, so "sign out
-- everywhere" reports FAILURE and the session KEEPS LIVING. That is fail-OPEN, out
-- of a mechanism whose purpose was to make revocation stronger. Rule: every write
-- to revoked_at carries COALESCE(revoked_at, ...) or an `IS NULL` predicate.
UPDATE admin_sessions
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = @id
  AND tenant_id = @tenant_id
RETURNING id, revoked_at;

-- name: RevokeAdminSessionsForAdmin :many
-- Revokes EVERY live session of one admin and returns the ids it revoked (empty
-- when there was nothing live -- idempotent). This is "sign out everywhere": the
-- password was changed or reset (M7-04), the laptop was lost, or a manager was
-- disabled and the person is still holding a cookie.
--
-- Unlike the employee equivalent, calling this on DISABLE is not merely allowed but
-- pointless-to-skip in only one direction: TouchAdminSession already refuses a
-- disabled admin on the next request, so revocation adds promptness and an audit
-- trail rather than the protection itself. It costs nothing to call and the
-- panel should.
UPDATE admin_sessions
SET revoked_at = now()
WHERE tenant_id = @tenant_id
  AND admin_user_id = @admin_user_id
  AND revoked_at IS NULL
RETURNING id;

-- name: ListAdminSessionsForAdmin :many
-- Every session of one admin, newest first: the read side of "sign out everywhere"
-- and of the panel's device list. Revoked rows are included on purpose; the caller
-- filters on revoked_at, so the history stays visible (section 4.6).
-- token_hash is NOT selected: nothing outside the resolution path needs it.
SELECT id, tenant_id, admin_user_id, created_at, last_used_at, revoked_at
FROM admin_sessions
WHERE tenant_id = @tenant_id
  AND admin_user_id = @admin_user_id
ORDER BY created_at DESC;

-- name: GetAdminByID :one
-- The in-context identity read: who am I, what may I do, am I still enabled.
-- This is where `role` comes from -- authorization is decided with a tenant
-- context established, never from the pre-authentication resolver (00011).
--
-- status is returned rather than filtered, for the same reason the resolvers carry
-- their state instead of hiding it: a caller that finds a disabled admin can say
-- so and audit it; one that gets "not found" cannot tell that from a bad id.
-- password_hash is NOT selected (section 4.7 spirit: it leaves the database only
-- on the one path that must compare it).
SELECT id, tenant_id, full_name, email, role, status, created_at, last_login_at
FROM admin_users
WHERE id = @id
  AND tenant_id = @tenant_id;

-- name: GetAdminForTenantChoice :one
-- One row of the "which business?" picker (the user decision behind 00011): the
-- BUSINESS NAME plus this operator's identity within it. Run once per candidate
-- tenant returned by resolve_admin_by_email, each inside that tenant's own
-- context, and ONLY AFTER the password has been verified -- the tenant name is an
-- enumeration signal and must never be reachable before authentication, which is
-- precisely why the resolver does not return it and does not touch `tenants`.
--
-- Both tables are RLS-covered by the same context, so the join cannot cross a
-- tenant boundary, and the explicit predicates are the belt on top.
--
-- The status = 'active' filter is what makes this query safe to drive a picker: a
-- disabled admin's tenant is simply not offered, so the picker cannot present a
-- door that CreateAdminSession would then refuse to open.
--
-- ⚠️ THE PICKER'S OTHER O(N), named here because it is the one that SURVIVES the
-- bcrypt bound. "Run once per candidate tenant" means one TENANT-SCOPED TRANSACTION
-- per candidate -- 500 candidates, 500 transactions, each with its own
-- SET LOCAL app.tenant_id, because a single statement cannot span two tenant
-- contexts. Risk is LOW today and stays low: it runs only AFTER a password has
-- verified, so it is not credential-less work like PHASE B OBLIGATION 4's bcrypt
-- loop. But once phase B bounds that loop, this is the remaining linear cost in the
-- login path, and it is the DATABASE's side of the line rather than the CPU's.
-- Phase B should size it against the same measurement it uses for the bcrypt bound;
-- the natural answer is that the picker only ever sees the candidates whose hash
-- VERIFIED (OBLIGATION 5), which bounds this loop as a side effect.
SELECT a.id, a.tenant_id, a.role, a.full_name, t.name AS tenant_name
FROM admin_users a
JOIN tenants t ON t.id = a.tenant_id
WHERE a.id = @id
  AND a.tenant_id = @tenant_id
  AND a.status = 'active';

-- name: MarkAdminLoggedIn :one
-- Stamps last_login_at after a SUCCESSFUL login. The status test keeps the stamp
-- honest: a disabled admin never gets one, so "last successful login" cannot be
-- advanced by an attempt that did not produce a session.
--
-- Unlike employees.activated_at this stamp is DELIBERATELY overwritten every time:
-- it answers "when was the most recent login", not "when was the first". The
-- history of logins is audit_log's job (phase B writes it), not this column's.
UPDATE admin_users
SET last_login_at = now()
WHERE id = @id
  AND tenant_id = @tenant_id
  AND status = 'active'
RETURNING id, last_login_at;

-- name: CreateAdminUser :one
-- Creates a panel operator. THE FIRST INSERT INTO admin_users ANY APPLICATION PATH
-- IN THIS PRODUCT HAS EVER MADE (M7-02) -- internal/adminauth/password.go names this
-- task as one of the three that open the write path, and states the rule it must
-- keep: "admin_users.password_hash is only ever written with the output of
-- adminauth.Hash".
--
-- 🔴 WHY IT IS AN INSERT ... SELECT RATHER THAN AN INSERT ... VALUES. CreateLocation
-- learned this the hard way and says so at length: an INSERT ... VALUES has no WHERE
-- and cannot have one, so the section 4.5 belt scanner
-- (internal/domain/*/query_test.go) finds no predicate to check and reports that it
-- verified NOTHING. Selecting the tenant row gives the statement a scoped source, so
-- the belt reads it like any other query -- subject `tenants`, whose scope column is
-- its own id (migration 00001's policy).
--
-- IT ALSO BUYS A REAL GUARANTEE and not merely a satisfied scanner: the INSERT cannot
-- happen unless the caller's tenant row is VISIBLE under RLS. In the sign-up flow that
-- row was written moments earlier in the SAME transaction under the SAME context, so
-- this is a genuine check that the two writes agree on which business they are for --
-- if provisioning ever passed two different ids, this query inserts nothing and the
-- whole registration rolls back rather than producing an admin belonging to a tenant
-- that is not theirs.
--
-- password_hash IS WRITTEN AND IS NEVER RETURNED. This file's header makes the
-- grep-checkable claim that password_hash is selected by no query here; that stays
-- true -- the value appears once, as a parameter of the INSERT's select-list, exactly
-- as @token_hash does in CreateAdminSession, and the RETURNING list below does not
-- name it. The generated *Row struct therefore has no PasswordHash field, so a row
-- read cannot leak the digest into a log.
--
-- email IS NOT RETURNED EITHER, for a smaller reason: nothing in the sign-up flow
-- needs it back (the boundary already holds the value it just sent) and an address is
-- personal data that would otherwise travel through a log line at the one moment the
-- request is still unauthenticated.
--
-- role IS A PARAMETER RATHER THAN A LITERAL 'owner', even though the sign-up wizard
-- is the only caller today and always passes 'owner'. M7-04's admin invitation is the
-- second caller and passes 'manager'; migration 00006's CHECK is the authority on
-- which values exist, so a Go-side literal here would be a second one.
INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role)
SELECT t.id, @full_name, @email, @password_hash, @role
FROM tenants t
WHERE t.id = @tenant_id
RETURNING id, tenant_id, full_name, role, status, created_at;
