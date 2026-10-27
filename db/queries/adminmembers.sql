-- adminmembers.sql -- the owner's side of a business's memberships (M14-16, ADR 0031 §5.12): the
-- people who run this business in the panel, disabling and enabling one, and moving a membership
-- between owner and manager (the two steps of an ownership transfer).
--
-- Every statement runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md §4.5,
-- belt and braces). Nothing here names identity_id, linked_via, linked_at or unlinked_at (R16 --
-- tappa_app reads only the first, and only TouchAdminSession may select it): a membership is managed
-- per business (K-6), and no statement here reaches the person's identity or another business.
--
-- THE LAST-OWNER INVARIANT IS THE CALLER'S (internal/adminauth.Memberships), under the lock the first
-- statement below takes: every active owner row of the business, in id order, FOR NO KEY UPDATE --
-- the strength a login's CreateAdminSession takes on a row, so two membership changes of one
-- business serialise on the owners and a later one re-reads what an earlier one committed (READ
-- COMMITTED re-checks a locked row's WHERE against its newest version).

-- name: LockActiveAdminOwners :many
-- The business's active owners, locked in id order (one order for every caller: no lock cycle).
SELECT id
FROM admin_users
WHERE tenant_id = @tenant_id
  AND role = 'owner'
  AND status = 'active'
ORDER BY id
FOR NO KEY UPDATE;

-- name: LockAdminMember :one
-- One membership of this business, locked; no row: not a membership here.
SELECT id, role, status
FROM admin_users
WHERE tenant_id = @tenant_id
  AND id = @id
FOR NO KEY UPDATE;

-- name: ListAdminMembers :many
-- The owner's list: owners first, then by age. Bounded: a business's panel people are a handful.
SELECT id, full_name, email, role, status, created_at, last_login_at
FROM admin_users
WHERE tenant_id = @tenant_id
ORDER BY (role = 'owner') DESC, created_at, id
LIMIT 200;

-- name: SetAdminMemberStatus :one
-- active <-> disabled. No row when the membership is not here or already holds @status.
UPDATE admin_users
SET status = @status
WHERE tenant_id = @tenant_id
  AND id = @id
  AND status <> @status
RETURNING id;

-- name: SetAdminMemberRole :one
-- owner <-> manager, on an ACTIVE membership only. No row when it is not here, not active, or
-- already holds @role.
UPDATE admin_users
SET role = @role
WHERE tenant_id = @tenant_id
  AND id = @id
  AND status = 'active'
  AND role <> @role
RETURNING id;
