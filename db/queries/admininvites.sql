-- admininvites.sql -- the owner's side of an administrator invitation (M14-15, ADR 0031 §5.11;
-- migration 00077). The owner issues, lists and cancels its own business's invitations.
--
-- Every statement here runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md
-- §4.5, belt and braces). The acceptance is NOT here: it is keyed by the invitation token and runs
-- through ADR 0031 §5.2's identity_accept_invite / identity_accept_invite_new (hand-written in
-- internal/db/identity.go -- sqlc cannot type a RETURNS TABLE call).
--
-- 🔴 NOTHING HERE READS A MEMBERSHIP OR AN IDENTITY (R18). The owner's answer, its timing and the
-- list's state must be one shape whether or not the address already has a Taptime sign-in, so the
-- issuing path writes the invitation and reads nothing else; token_hash is not readable by tappa_app
-- and is not named.

-- name: CancelOpenAdminInvitesForAddress :execrows
-- The address's open invitation in this business, if any, before a new one is issued (one open
-- invitation per address per business -- the partial UNIQUE admin_invites_one_open_per_address).
-- An expired one is open too until it is cancelled or spent, so it is retired here as well.
UPDATE admin_invites
SET cancelled_at = clock_timestamp()
WHERE tenant_id = @tenant_id
  AND email = @email
  AND used_at IS NULL
  AND cancelled_at IS NULL;

-- name: InsertAdminInvite :one
-- id, created_at, used_at and cancelled_at are not tappa_app's to write (00077). The row is written
-- FROM the inviter's own membership row: an active OWNER of this business (the panel's policy gate
-- decided first; this is the belt -- no row, nothing written). tenant_id is the source row's own.
INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
SELECT a.tenant_id, @email, @full_name, @role, @token_hash, a.id, @expires_at
FROM admin_users a
WHERE a.tenant_id = @tenant_id
  AND a.id = @invited_by
  AND a.role = 'owner'
  AND a.status = 'active'
RETURNING id, created_at, expires_at;

-- name: ListAdminInvites :many
-- The owner's list: newest first, at most 50, spent and cancelled ones included (the owner sees
-- what was sent and what became of it).
SELECT id, email, full_name, role, invited_by, created_at, expires_at, used_at, cancelled_at
FROM admin_invites
WHERE tenant_id = @tenant_id
ORDER BY created_at DESC, id
LIMIT 50;

-- name: CancelAdminInvite :one
-- One open invitation, by its id in this business. No row: unknown here, spent, or cancelled
-- already -- the caller writes no audit row then.
UPDATE admin_invites
SET cancelled_at = clock_timestamp()
WHERE tenant_id = @tenant_id
  AND id = @id
  AND used_at IS NULL
  AND cancelled_at IS NULL
RETURNING id, expires_at;
