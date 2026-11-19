-- liveqr.sql -- the live QR screens (M16-19, ADR 0045 §2.5; migration 00111). M16-20 pairs a screen
-- from the panel and serves its page; M16-21 resolves a tap's screen.
--
-- Every statement here runs inside WithTenant and carries an explicit tenant_id filter (CLAUDE.md
-- §4.5, belt and braces). The three context-less reads of live_qr_displays are the resolvers,
-- hand-written in internal/db/resolve.go (sqlc cannot type a RETURNS TABLE call -- resolve.sql). They
-- are NOT duplicated here: the screen page's request names a code digest or a session digest and no
-- tenant, a resolver answers the tenant, and only then do these run.
--
-- KIRMIZI CIZGI (§4.7, §7): no query RETURNS pairing_code_hash or session_hash -- tappa_app cannot
-- SELECT the second at all (00111), and the first exists to be matched. The code and the cookie
-- value never reach the database: their digests are computed by the caller (M16-20).
--
-- 🔴 EVERY WRITE TO revoked_at IS COALESCE(revoked_at, now()) (appdevices.sql's note): the
-- live_qr_displays_state_one_way trigger refuses a revoked_at that moves, so a bare
-- `SET revoked_at = now()` would reach it the moment two revocations race and roll the winner's
-- caller back. With COALESCE the loser re-reads the committed row and writes nothing new.

-- name: CreateLiveQRDisplay :one
-- Records a screen waiting for its pairing code at one location (the panel's "Add a live QR screen",
-- M16-20). tenant_id is explicit and the RLS WITH CHECK confirms it equals the context; the composite
-- key to locations confirms the location is the same tenant's. ttl_seconds is the caller's; the
-- table's CHECK caps it at one hour (23514 above that). id and created_at are the database's. A code
-- digest another waiting screen already holds -- any tenant -- is refused by
-- live_qr_displays_pairing_code_hash_key (23505): the caller draws another code.
INSERT INTO live_qr_displays (tenant_id, location_id, pairing_code_hash, pairing_expires_at)
VALUES (@tenant_id, @location_id, @pairing_code_hash::text, now() + make_interval(secs => @ttl_seconds::int))
RETURNING id, location_id, pairing_expires_at, created_at;

-- name: ConsumeLiveQRDisplayPairing :one
-- THE PAIRING, ONE STATEMENT (§4.4's shape; the card: the code is consumed by one statement): the
-- code's digest is cleared, the pairing stamped and the screen session's digest stored, only while
-- the code is unconsumed, unexpired and the screen unrevoked. 0 rows (pgx.ErrNoRows) -> the code is
-- refused, whatever the reason (the caller says one thing for all of them). Two racing consumptions
-- of one code: the second waits for the first's row lock, re-reads the committed row -- its
-- pairing_code_hash is NULL now -- and matches nothing (TestLiveQRDisplays_ConcurrentPairingExactlyOnce).
-- A revocation racing it is the same row: whichever commits first, the other matches nothing new.
UPDATE live_qr_displays
SET pairing_code_hash = NULL,
    paired_at         = now(),
    session_hash      = @session_hash::text
WHERE tenant_id = @tenant_id
  AND id = @id
  AND pairing_code_hash = @pairing_code_hash::text
  AND paired_at IS NULL
  AND revoked_at IS NULL
  AND pairing_expires_at > now()
RETURNING id, location_id, paired_at;

-- name: RevokeLiveQRDisplay :one
-- Revokes one screen (the panel's revoke, M16-20). COALESCE keeps the FIRST stamp, so a repeat is
-- idempotent and never rewrites when the screen died. The row is matched whatever its state, so
-- pgx.ErrNoRows means "no such screen in this tenant" -- a different answer from "already revoked".
-- A screen waiting for its code is revoked the same way, and its code's digest is DROPPED in the same
-- statement: a dead code must not hold its value in the global UNIQUE (00111's header). A paired
-- screen holds no code, so the second assignment changes nothing there.
UPDATE live_qr_displays
SET revoked_at        = COALESCE(revoked_at, now()),
    pairing_code_hash = NULL
WHERE tenant_id = @tenant_id
  AND id = @id
RETURNING id, revoked_at;

-- name: ListLiveQRDisplaysForLocation :many
-- The screens of one location, newest first (the panel's list, M16-20): when each was created,
-- whether it is waiting for its code (paired_at NULL, until pairing_expires_at), paired, or revoked.
-- No digest is returned.
SELECT id, location_id, created_at, pairing_expires_at, paired_at, revoked_at
FROM live_qr_displays
WHERE tenant_id = @tenant_id
  AND location_id = @location_id
ORDER BY created_at DESC, id;

-- name: ClearExpiredLiveQRPairingCodes :execrows
-- Drops the code digest of this tenant's screens whose pairing expired before it was used (00111's
-- header: a dead code must not hold its value in the global UNIQUE). M16-20 runs it in the creating
-- tenant before it draws a new code, and any cleanup may. The screens stay -- dead unpaired, listed
-- by ListLiveQRDisplaysForLocation -- and never pair: the one-way trigger refuses a dropped code back.
-- Tenant-scoped like every lazy delete here (K-19): another tenant's expired codes wait for that
-- tenant.
UPDATE live_qr_displays
SET pairing_code_hash = NULL
WHERE tenant_id = @tenant_id
  AND paired_at IS NULL
  AND pairing_code_hash IS NOT NULL
  AND pairing_expires_at <= now();
