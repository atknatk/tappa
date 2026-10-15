-- invitequeue.sql -- K-17's invitation queue (M11 API-6; ADR 0028 §9, ADR 0022's M11 note).
--
-- WHAT A ROW IS. invite_requests (00037) holds ONE request per employee -- (tenant_id,
-- employee_id) is its key -- and nothing else than when it was asked for (requested_at, the
-- DEFAULT), the lease (lease_until) and the try count (attempts). 🔴 NO CODE AND NO HASH OF ONE:
-- a code is minted at SEND time, inside the worker's minting transaction (internal/invite,
-- queue.go), and never waits in a table. internal/invite's queue test reads the table's columns
-- and the worker's log, trail and queue rows to say so (ADR 0028 "API-6 uygulama notu").
--
-- WHO WRITES IT. The API (internal/domain/tenant, staffapi.go) asks: a new employee with an
-- address (K-16), an address change of somebody not activated yet (K-15), resend-activation.
-- The worker (internal/invite) leases, mints, sends and deletes. tappa_app holds SELECT, INSERT
-- (tenant_id, employee_id), UPDATE (lease_until, attempts) and DELETE (00037) -- requested_at
-- cannot be re-dated and a request cannot be moved to another person.
--
-- THE LEASE (K-17: 60 seconds; two pods never mint the same invitation twice). lease_until is
-- also the "not before" of a deferred request (a limit reached, a failed send's back-off): a row
-- is DUE when lease_until IS NULL or has passed. A pod claims ONE due row at a time (SKIP LOCKED,
-- so a second pod takes another row or none) and renews the lease INSIDE its minting
-- transaction, so the lease covers the send (10 s) and its audit row (5 s) from the mint on.
--
-- TENANT SCOPE (section 4.5, belt + braces): every statement binds tenant_id to @tenant_id as a
-- top-level conjunct of its own WHERE (or as the VALUE an INSERT writes), on top of RLS. K-19:
-- the worker is handed ONE tenant and reads only its rows -- nothing here scans across tenants.

-- name: EnqueueInviteRequest :one
-- Puts the employee into the queue, or leaves the request that is already there as it is (plan
-- §2 item 12: a second ask while one waits is a no-op). requested_at is the DEFAULT. Returns
-- whether a row was inserted.
INSERT INTO invite_requests (tenant_id, employee_id)
VALUES (@tenant_id, @employee_id)
ON CONFLICT (tenant_id, employee_id) DO NOTHING
RETURNING employee_id;

-- name: RequeueInviteRequest :exec
-- Puts the employee into the queue AFTER AN ADDRESS CHANGE (K-15), and -- unlike
-- EnqueueInviteRequest -- RELEASES a request that is already there: its lease is cleared and its
-- tries reset. A worker that minted for the OLD address before the change committed sends a
-- link the change has already retired; its lease is gone, so it cannot take the request out of
-- the queue afterwards (FinishInviteRequest matches its own lease only) and the next run mints
-- for the new address. tenant_id is not in the SET list.
INSERT INTO invite_requests (tenant_id, employee_id)
VALUES (@tenant_id, @employee_id)
ON CONFLICT (tenant_id, employee_id) DO UPDATE
SET lease_until = NULL,
    attempts    = 0
WHERE invite_requests.tenant_id = @tenant_id;

-- name: DeleteInviteRequest :exec
-- Takes the employee out of the queue whatever its lease: they were deactivated, or their
-- address was removed -- nothing is left to send.
DELETE FROM invite_requests
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id;

-- name: GetInviteRequest :one
-- Whether a request for the employee waits in the queue (resend-activation's first no-op).
SELECT requested_at
FROM invite_requests
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id;

-- name: CountRecentLiveInvites :one
-- resend-activation's second no-op (ADR 0028 §9): an invitation minted in the last hour that can
-- still be spent -- not used, not retired, not expired. "Sent" is read off the row the worker
-- (or the panel) minted; created_at is the database's clock.
SELECT count(*)::int AS live
FROM employee_invites
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND used_at IS NULL
  AND cancelled_at IS NULL
  AND now() < expires_at
  AND created_at > now() - interval '1 hour';

-- name: ClaimInviteRequest :one
-- The worker's lease: the OLDEST due request of the tenant, taken for 60 seconds. SKIP LOCKED:
-- a second pod claiming at the same moment skips this row and takes another or none -- the two
-- never hold one request together. The lease is clock_timestamp()-based (not the transaction's
-- start), so it is the instant this statement ran; the worker keeps the returned value and
-- matches it at every later step.
UPDATE invite_requests r
SET lease_until = clock_timestamp() + interval '60 seconds',
    attempts    = r.attempts + 1
WHERE r.tenant_id = @tenant_id
  AND r.employee_id = (
      SELECT q.employee_id
      FROM invite_requests q
      WHERE q.tenant_id = @tenant_id
        AND (q.lease_until IS NULL OR q.lease_until <= clock_timestamp())
      ORDER BY q.requested_at, q.employee_id
      LIMIT 1
      FOR UPDATE SKIP LOCKED)
RETURNING r.employee_id, r.lease_until, r.attempts;

-- name: RenewInviteRequestLease :one
-- Inside the minting transaction, after the invitation is inserted: the lease is still THIS
-- worker's (nobody re-queued or re-claimed the request) and runs 60 seconds from now, so it
-- covers the send and its audit row. No row: the lease was lost -- the caller rolls the mint
-- back.
UPDATE invite_requests
SET lease_until = clock_timestamp() + interval '60 seconds'
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND lease_until = @lease_until
RETURNING lease_until;

-- name: FinishInviteRequest :one
-- The request is done (sent, or given up): it leaves the queue -- but only if the lease is still
-- this worker's. A request an address change re-queued in the meantime (RequeueInviteRequest)
-- stays, and the next run mints for the new address.
DELETE FROM invite_requests
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND lease_until = @lease_until
RETURNING employee_id;

-- name: DeferInviteRequest :one
-- Not now: a limit is reached (ADR 0022 §9 -- the limit delays the send, never refuses the
-- request) or the send failed (back-off). The request stays, due again at @due. Only under this
-- worker's lease.
UPDATE invite_requests
SET lease_until = @due
WHERE tenant_id = @tenant_id
  AND employee_id = @employee_id
  AND lease_until = @lease_until
RETURNING lease_until;

-- name: GetEmployeeInviteStatus :one
-- The person's lifecycle state, read BEFORE the minting transaction retires anything: only an
-- 'invited' person is sent an invitation by the queue (K-15: an activated person gets none; a
-- deactivated one could never spend it).
SELECT status
FROM employees
WHERE tenant_id = @tenant_id
  AND id = @employee_id;

-- name: LockEmployeeForQueuedInvite :one
-- The same state, FOR SHARE, inside the minting transaction after the retirements and before
-- the insert: FOR SHARE conflicts with the FOR NO KEY UPDATE an activation's UPDATE employees
-- and a deactivation take, and with the FOR UPDATE an address change takes, so none of them can
-- commit between this read and the mint's commit. The lock order is the activation's and
-- ChangeEmail's -- invitations first, then the employee -- so none of the three deadlocks
-- against it.
SELECT status
FROM employees
WHERE tenant_id = @tenant_id
  AND id = @employee_id
FOR SHARE;
