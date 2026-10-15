-- webhooks.sql -- the owner's webhook endpoint (M11 API-9, ADR 0028 §6; table from migration 00048).
--
-- Every statement runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md §4.5,
-- belt and braces). tappa_app holds SELECT, INSERT (tenant_id, id, url, signing_key_sealed, active)
-- and UPDATE (url, signing_key_sealed, active, rotated_at) on the table -- never DELETE, never
-- tenant_id (00048, ADR 0028 §1's matrix).
--
-- 🔴 THE SEALED KEY IS READ BY ONE STATEMENT: GetWebhookEndpointForSigning, for the ping (and, with
-- API-10, the delivery). The panel's read names no key column. The url is never logged by any
-- caller (CLAUDE.md §7).

-- name: GetWebhookEndpoint :one
-- The panel's read: the business's one endpoint (UNIQUE (tenant_id)), without its key.
SELECT id, url, active, created_at, rotated_at
FROM webhook_endpoints
WHERE tenant_id = @tenant_id;

-- name: GetWebhookEndpointForSigning :one
-- The endpoint with its sealed key, for a request that signs (the ping). The key is opened with the
-- row's own id as AAD, so a sealed value moved to another row does not open.
SELECT id, url, signing_key_sealed, active
FROM webhook_endpoints
WHERE tenant_id = @tenant_id;

-- name: InsertWebhookEndpoint :one
-- The id is chosen by the caller BEFORE the insert: it is the sealed key's AAD. created_at and
-- rotated_at are the table's (default, NULL). A second endpoint of the business meets
-- webhook_endpoints_one_per_tenant: a unique violation, SQLSTATE 23505.
INSERT INTO webhook_endpoints (tenant_id, id, url, signing_key_sealed, active)
VALUES (@tenant_id, @id, @url, @signing_key_sealed, true)
RETURNING created_at;

-- name: RotateWebhookEndpointKey :one
-- A new sealed key for THIS endpoint (the id is the seal's AAD -- a key sealed for another id would
-- not open here), and the moment it changed. The old key no longer signs anything from the commit on.
UPDATE webhook_endpoints
SET signing_key_sealed = @signing_key_sealed,
    rotated_at = clock_timestamp()
WHERE tenant_id = @tenant_id
  AND id = @id
RETURNING rotated_at;

-- name: ReaddressWebhookEndpoint :execrows
-- A new address for THIS endpoint; its key is unchanged.
UPDATE webhook_endpoints
SET url = @url
WHERE tenant_id = @tenant_id
  AND id = @id;

-- name: SetWebhookEndpointActive :execrows
-- Switch the endpoint off or on. Only a change matches: switching off an endpoint that is off writes
-- nothing, so the caller writes no second trail row.
UPDATE webhook_endpoints
SET active = @active
WHERE tenant_id = @tenant_id
  AND id = @id
  AND active <> @active;

-- name: LockWebhookBucket :one
-- SERIALISE ONE BUSINESS'S PINGS (and, with API-10, its "resend"s -- ADR 0028 §6: one bucket, ten an
-- hour). The bucket is a READ-THEN-DECIDE over audit_log (ReadWebhookBucket), and without a lock N
-- concurrent pings all count before any has committed and all pass. LockAPIDeactivationBudget's
-- shape and argument, verbatim: the lock is taken in the SELECT LIST of this business's tenants row
-- (RLS and the explicit predicate), so a context in which the row is not visible returns NO ROW and
-- takes no lock, which the caller treats as a failure. The namespace keeps it apart from every other
-- advisory key.
SELECT t.id,
       (pg_advisory_xact_lock(hashtextextended('webhook-bucket:' || (sqlc.arg(tenant_id)::uuid)::text, 0)) IS NULL)::boolean AS unlocked
FROM tenants t
WHERE t.id = @tenant_id;

-- name: ReadWebhookBucket :one
-- How many slots of the bucket this business spent in the last hour, and when the oldest was spent
-- (the next slot frees an hour after it). A slot is a trail row of one of @actions -- the ping's row
-- is written BEFORE the request goes out, in the transaction that holds the lock, so a slot is spent
-- whether or not the remote answers and a crash after the commit cannot give it back. `at` is the
-- database's clock (00053: tappa_app cannot write it). audit_log_tenant_at_idx serves the range.
--
-- THE WINDOW HAS ONE SOURCE: @window_seconds is endpoints.BucketWindow, the same constant that dates
-- the next free slot. make_interval keeps the bound a bare parameter (CLAUDE.md §6, as
-- audit.sql's ConfirmRecentRemoval does) -- now() is evaluated once per statement, so `at > <constant>` stays the index condition
-- under the generic plan. TestWebhookPing_CountsTheLastHourOnly dates slots 59 and 61 minutes back.
SELECT count(*)::int AS spent,
       coalesce(min(at), now())::timestamptz AS oldest
FROM audit_log
WHERE tenant_id = @tenant_id
  AND action = ANY (@actions::text[])
  AND at > now() - make_interval(secs => @window_seconds::int);

-- ---------------------------------------------------------------------------------------------
-- THE DELIVERIES (M11 API-10, ADR 0028 §6). A webhook_deliveries row is written by the punch feed's
-- triggers (00049: one per target event whose business has its API open and its endpoint on); the
-- worker (internal/webhook/delivery) leases it, sends it OUTSIDE any transaction and records the
-- result; the owner reads the log and asks for a resend (internal/webhook/endpoints). tappa_app
-- holds SELECT and UPDATE (lease_until, attempt, next_attempt_at, state, last_status,
-- last_error_class, delivered_at) -- no INSERT, no DELETE (00048). K-19: every statement is handed
-- ONE business and carries its tenant_id as a top-level conjunct; nothing here reads across
-- businesses.
-- ---------------------------------------------------------------------------------------------

-- name: ClaimWebhookDelivery :one
-- THE LEASE (ADR 0028 §6, step 1): the business's ONE oldest-due delivery -- pending, its
-- next_attempt_at reached, no live lease -- taken for @lease_seconds. SKIP LOCKED: a second worker
-- claiming at the same moment skips the row this one holds and takes another or none; the lease
-- keeps it from a later claim until it ends (a worker that died mid-send leaves it to expire, and the
-- delivery is tried again). ORDER BY next_attempt_at alone, so webhook_deliveries_tenant_due_idx
-- (tenant_id, next_attempt_at) WHERE state = 'pending' hands the first row without a sort; there is
-- no order guarantee to keep (plan §2 item 16). THE DUE BOUND IS statement_timestamp(), NOT
-- clock_timestamp(): a stable function is an Index Cond, a volatile one only a Filter -- and a claim that
-- finds nothing due (every run's last) then walked EVERY pending row of the business (measured, 5 000
-- pending all due in an hour: 85 buffers as a Filter, 2 as an Index Cond; ADR 0028 "API-10 uygulama
-- notu"). The statement's start is microseconds from its clock; the lease stays on clock_timestamp(). attempt is NOT counted here: a claim is not a try
-- (a closed gate gives the row back untouched). The lease is clock_timestamp()-based; the worker
-- keeps the returned value and matches it when it records the result.
UPDATE webhook_deliveries d
SET lease_until = clock_timestamp() + make_interval(secs => @lease_seconds::int)
WHERE d.tenant_id = @tenant_id
  AND d.id = (
      SELECT q.id
      FROM webhook_deliveries q
      WHERE q.tenant_id = @tenant_id
        AND q.state = 'pending'
        AND q.next_attempt_at <= statement_timestamp()
        AND (q.lease_until IS NULL OR q.lease_until <= clock_timestamp())
      ORDER BY q.next_attempt_at
      LIMIT 1
      FOR UPDATE SKIP LOCKED)
RETURNING d.id, d.endpoint_id, d.punch_event_seq, d.attempt, d.lease_until;

-- name: GetPunchEventForDelivery :one
-- The event a delivery carries, in the delivering business's own feed (a delivery points at its
-- tenant's (tenant_id, seq) -- 00048's webhook_deliveries_event_fk). The snapshot columns only: the
-- table has no address, coordinate, note, policy context or verdict to read.
SELECT seq, punch_id, version, status, punch_time, recorded_at, channel, direction,
       employee_id, employee_ref, employee_code, employee_company,
       location_id, location_ref, location_name, location_company, tag_uid
FROM punch_events
WHERE tenant_id = @tenant_id
  AND seq = @seq;

-- name: RecordWebhookDeliveryResult :one
-- THE RESULT (ADR 0028 §6, step 3), in a short transaction of its own -- and only under THIS
-- worker's lease: a row whose lease expired and was claimed again (or resent) is not this worker's
-- any more, and matches nothing. The caller has decided the state (delivered, pending with
-- @delay_seconds of back-off, dead) from the class and the claimed attempt; the remote's text is
-- never here (ADR 0028 §6: only the closed class and the status). The lease ends with the result.
UPDATE webhook_deliveries
SET attempt          = attempt + 1,
    state            = sqlc.arg(state)::text,
    next_attempt_at  = CASE WHEN sqlc.arg(state)::text = 'pending'
                            THEN clock_timestamp() + make_interval(secs => @delay_seconds::int)
                            ELSE next_attempt_at END,
    last_status      = sqlc.narg(last_status)::int,
    last_error_class = sqlc.narg(last_error_class)::text,
    delivered_at     = CASE WHEN sqlc.arg(state)::text = 'delivered' THEN clock_timestamp() ELSE delivered_at END,
    lease_until      = NULL
WHERE tenant_id = @tenant_id
  AND id = @id
  AND state = 'pending'
  AND lease_until = @lease_until
RETURNING attempt, state, next_attempt_at;

-- name: NextWebhookDeliveryDue :one
-- When the business's next pending delivery comes due -- its back-off's end, or a live lease's end
-- (another worker's, or one that died) -- as whole seconds from now (0 or less: due now), and how
-- many are pending (0: nothing waits, and due_in_seconds means nothing). K-19: the worker notices
-- the business again then, a timer for ONE business, never a scan.
SELECT count(*)::int AS pending,
       coalesce(ceil(extract(epoch FROM min(greatest(q.next_attempt_at, coalesce(q.lease_until, q.next_attempt_at))) - clock_timestamp())), 0)::bigint AS due_in_seconds
FROM webhook_deliveries q
WHERE q.tenant_id = @tenant_id
  AND q.state = 'pending';

-- name: ListWebhookDeliveries :many
-- THE DELIVERY LOG (the owner's panel): the business's deliveries, newest first, a page at a time.
-- The position is the last row shown, (created_at, id): every row strictly before it in that order.
-- created_at <= @before_at is the index condition (webhook_deliveries_tenant_created_idx); the id
-- breaks a tie at one instant. The punch's id comes from the business's OWN feed row (the event the
-- delivery carries); nothing of the endpoint's address is read.
SELECT d.id, d.punch_event_seq, d.attempt, d.next_attempt_at, d.lease_until, d.state, d.last_status,
       d.last_error_class, d.created_at, d.delivered_at, e.punch_id,
       (d.lease_until IS NOT NULL AND d.lease_until > clock_timestamp())::boolean AS in_flight
FROM webhook_deliveries d
JOIN punch_events e ON e.tenant_id = d.tenant_id AND e.seq = d.punch_event_seq
WHERE d.tenant_id = @tenant_id
  AND e.tenant_id = @tenant_id
  AND d.created_at <= @before_at
  AND (d.created_at < @before_at OR d.id < @before_id)
ORDER BY d.created_at DESC, d.id DESC
LIMIT @page_size;

-- name: LockWebhookDeliveryForResend :one
-- The owner's "resend" (ADR 0028 §6): the delivery, locked for the act's transaction, and whether a
-- worker holds a live lease on it right now (a resend then would race the send in flight).
SELECT id, endpoint_id, state,
       (lease_until IS NOT NULL AND lease_until > clock_timestamp())::boolean AS in_flight
FROM webhook_deliveries
WHERE tenant_id = @tenant_id
  AND id = @id
FOR UPDATE;

-- name: ResendWebhookDelivery :execrows
-- Makes a pending or dead delivery due NOW. attempt is kept: a dead delivery (its sixth try spent)
-- revived by a resend gets ONE try -- failing, it is dead again -- so one bucket slot is one request;
-- a pending one keeps the rest of its schedule. Not under a live lease (the caller refused that).
UPDATE webhook_deliveries
SET state = 'pending',
    next_attempt_at = clock_timestamp(),
    lease_until = NULL
WHERE tenant_id = @tenant_id
  AND id = @id
  AND state IN ('pending', 'dead')
  AND (lease_until IS NULL OR lease_until <= clock_timestamp());
