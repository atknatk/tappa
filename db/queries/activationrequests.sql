-- activationrequests.sql -- the manager-approved activation (M16-23; migration 00115): a phone that
-- cannot tap a plaque asks, a manager approves or rejects in the panel, and the phone collects.
--
-- Every statement here runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md
-- §4.5, belt and braces). The phone's tenant is its invitation's (resolve_invite_by_code_hash), the
-- panel's its session's -- no statement here is context-less.
--
-- KIRMIZI CIZGI (§7): binding_hash is matched, never RETURNED; the pairing code is returned to the
-- two places that show it (the phone that asked, the panel) and to nothing else -- no log line and no
-- audit detail carries it (internal/invite, internal/handler).

-- name: CountLiveQRScreensForApproval :one
-- THE GATE (the card's user decision): the manager approval is offered only where no live QR screen
-- runs. A screen RUNS when it is paired, not revoked, its session younger than
-- liveqr.ScreenSessionLifetime (session_lifetime_seconds), its venue active, and it has asked for a
-- code within seen_within_seconds (last_seen_at, 00114 -- stamped at most every ten minutes while the
-- screen polls). A screen that stopped polling shows nothing to scan, so it does not close the door.
-- location_id NULL counts the whole business (a person with no home venue, K-14).
SELECT count(*)::int AS screens
FROM live_qr_displays d
JOIN locations l
  ON l.id = d.location_id
 AND l.tenant_id = d.tenant_id
WHERE d.tenant_id = @tenant_id
  AND (sqlc.narg('location_id')::uuid IS NULL OR d.location_id = sqlc.narg('location_id')::uuid)
  AND d.paired_at IS NOT NULL
  AND d.revoked_at IS NULL
  AND d.paired_at > now() - make_interval(secs => @session_lifetime_seconds::int)
  AND d.last_seen_at > now() - make_interval(secs => @seen_within_seconds::int)
  AND l.active;

-- name: WithdrawOpenActivationRequests :execrows
-- Opening a new request withdraws the invitation's open one first, in the same transaction
-- (00115's one-open UNIQUE): the newest request is the one the panel shows.
UPDATE activation_requests
SET withdrawn_at = now()
WHERE tenant_id = @tenant_id
  AND invite_id = @invite_id
  AND decision IS NULL
  AND withdrawn_at IS NULL;

-- name: CreateActivationRequest :one
-- Opens a request for the invitation's CURRENT consent: the row is written only while the invitation
-- is unused, uncancelled and unexpired and its consent binding is the caller's -- the INSERT selects
-- from the invitation, so a request is born only for the browser or key that consented last. 0 rows
-- (pgx.ErrNoRows): the invitation is not usable for this binding; the caller re-reads why. ttl_seconds
-- is the caller's; 00115's CHECK caps it at one hour.
INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at)
SELECT i.tenant_id, i.employee_id, i.id, @client::text, @binding_hash::text, @pairing_code::text,
       now() + make_interval(secs => @ttl_seconds::int)
FROM employee_invites i
WHERE i.tenant_id = @tenant_id
  AND i.id = @invite_id
  AND i.used_at IS NULL
  AND i.cancelled_at IS NULL
  AND i.expires_at > now()
  AND i.consent_binding_hash = @binding_hash::text
RETURNING id, pairing_code, created_at, expires_at;

-- name: GetActivationRequestForBinding :one
-- The phone's read of its own latest request: the invitation's request opened by THIS binding,
-- newest first. expired is the DATABASE clock's answer. pgx.ErrNoRows: this binding never asked.
SELECT id, pairing_code, created_at, expires_at, decision, decided_by, withdrawn_at, collected_at,
       (expires_at <= now())::boolean AS expired
FROM activation_requests
WHERE tenant_id = @tenant_id
  AND invite_id = @invite_id
  AND binding_hash = @binding_hash::text
ORDER BY created_at DESC, id
LIMIT 1;

-- name: CollectActivationApproval :one
-- THE COLLECTION, ONE STATEMENT: the approved, uncollected, unexpired request of this invitation and
-- binding is stamped collected. It runs inside the consuming transaction (internal/invite
-- ActivateApproved / ActivateDeviceApproved), so "the invitation was consumed on a manager's
-- approval" implies "that approval was collected, once": a second collection matches nothing, and a
-- rolled-back consumption un-collects nothing because nothing committed. pgx.ErrNoRows: no approval
-- to collect.
UPDATE activation_requests
SET collected_at = now()
WHERE tenant_id = @tenant_id
  AND invite_id = @invite_id
  AND binding_hash = @binding_hash::text
  AND decision = 'approved'
  AND collected_at IS NULL
  AND expires_at > now()
RETURNING id, decided_by;

-- name: ListOpenActivationRequests :many
-- The panel's "Phones waiting for approval": the business's open, unexpired requests whose
-- invitation can still be spent by the binding that asked (a request whose person has since
-- activated another way, or whose consent moved to another phone, is not shown -- approving it would
-- complete nothing). Oldest first. The venue is the person's home venue, NULL for none (K-14).
-- replaces_phone: the person holds a live browser session or a live app device right now, which
-- the approval's activation signs out (Q40 (a)) -- the panel row says so (M16-23 security DÜŞÜK).
-- "Live" is the panel card's own definition (GetPanelEmployeeSummary: revoked_at IS NULL).
SELECT r.id, r.employee_id, e.full_name, l.name AS location_name, r.client, r.pairing_code,
       r.created_at, r.expires_at,
       (EXISTS (SELECT 1 FROM sessions s
                 WHERE s.tenant_id = @tenant_id AND s.tenant_id = r.tenant_id AND s.employee_id = r.employee_id
                   AND s.revoked_at IS NULL)
        OR EXISTS (SELECT 1 FROM app_devices d
                    WHERE d.tenant_id = @tenant_id AND d.tenant_id = r.tenant_id AND d.employee_id = r.employee_id
                      AND d.revoked_at IS NULL)
       )::boolean AS replaces_phone
FROM activation_requests r
JOIN employees e
  ON e.id = r.employee_id
 AND e.tenant_id = r.tenant_id
JOIN employee_invites i
  ON i.id = r.invite_id
 AND i.tenant_id = r.tenant_id
LEFT JOIN locations l
  ON l.id = e.location_id
 AND l.tenant_id = e.tenant_id
WHERE r.tenant_id = @tenant_id
  AND r.decision IS NULL
  AND r.withdrawn_at IS NULL
  AND r.expires_at > now()
  AND i.used_at IS NULL
  AND i.cancelled_at IS NULL
  AND i.expires_at > now()
  AND i.consent_binding_hash = r.binding_hash
  AND e.status <> 'deactivated'
ORDER BY r.created_at, r.id;

-- name: LockActivationRequestForDecision :one
-- The panel's decision reads the request FOR UPDATE first, so two managers pressing at once queue on
-- the row: the second reads the committed decision and is answered "no longer open" (one audit row).
-- self is THE SELF-APPROVAL CHECK (ADR 0037's spirit, the card): the deciding panel account's
-- address is the person's address. admin_users.email is the account's address (an identity-linked
-- account's mirror, 00102), employees.email the person's; both citext, so the comparison ignores
-- case. A person without an address is never "self" -- the check can only see what is written.
-- employee_deactivated: a deactivated person's request is closed to a decision (the consumption would
-- refuse it anyway; the panel must not record an approval nothing can collect -- M16-23 security
-- review DÜŞÜK). pgx.ErrNoRows: no such request in this business.
SELECT r.id, r.employee_id, r.client, r.decision, r.withdrawn_at,
       (r.expires_at <= now())::boolean AS expired,
       (e.status = 'deactivated')::boolean AS employee_deactivated,
       (i.used_at IS NULL AND i.cancelled_at IS NULL AND i.expires_at > now()
        AND i.consent_binding_hash = r.binding_hash)::boolean AS invite_usable,
       (e.email IS NOT NULL AND a.email IS NOT NULL AND e.email = a.email)::boolean AS self
FROM activation_requests r
JOIN employees e
  ON e.id = r.employee_id
 AND e.tenant_id = r.tenant_id
JOIN employee_invites i
  ON i.id = r.invite_id
 AND i.tenant_id = r.tenant_id
LEFT JOIN admin_users a
  ON a.id = @admin_user_id
 AND a.tenant_id = r.tenant_id
WHERE r.tenant_id = @tenant_id
  AND r.id = @id
FOR UPDATE OF r;

-- name: DecideActivationRequest :one
-- THE DECISION, ONE STATEMENT, ONE WAY: an open, unexpired request is approved or rejected by one
-- panel account, stamped now. The one-way trigger refuses any later change. pgx.ErrNoRows: the
-- request is no longer open (decided, withdrawn or expired) -- the caller read it locked, so that is
-- the expiry crossing between the read and this statement.
UPDATE activation_requests
SET decision   = @decision::text,
    decided_at = now(),
    decided_by = @decided_by
WHERE tenant_id = @tenant_id
  AND id = @id
  AND decision IS NULL
  AND withdrawn_at IS NULL
  AND expires_at > now()
RETURNING id, employee_id, decision, decided_at;
