-- oauth.sql -- the OAuth 2.0 "Connected apps" statements (issue #37 phase 1; ADR 0047 §4.5, §5.2,
-- §5.3, §6, §8.1, §11.3, §12; migrations 00104-00106).
--
-- Every statement here runs inside WithTenant(the tenant the credential resolved to -- or the panel
-- session's) and carries an explicit tenant_id filter (CLAUDE.md §4.5, belt and braces). The
-- context-less reads are the three resolvers of 00105 (resolve_oauth_client,
-- resolve_oauth_code_by_hash, resolve_oauth_token_by_hash), hand-written in internal/db/resolve.go
-- (sqlc cannot type a RETURNS TABLE call -- resolve.sql). tappa_app reaches no row of oauth_clients:
-- an app's name and state come from resolve_oauth_client, never from a join here.
--
-- 🔴 EVERY "IT HAPPENED" WRITE IS ONE CONDITIONAL STATEMENT (CLAUDE.md §4.4's principle; 00011's
-- BOUNDARY 2): consumed_at, used_at and revoked_at are written only where they are still NULL (and
-- COALESCE where a statement could meet a stamped row), so two racing writers never reach 00104's
-- one-way triggers -- the loser matches no row and writes nothing.
--
-- 🔴 ONE CLOCK (ADR 0047 §14.2, ADR 0038): a row's created_at and expires_at are the database's
-- now() (the column DEFAULTs; oauth_tokens.expires_at is computed here, in the INSERT's text, from
-- now() and the kind -- never a parameter). Every "has it expired / is it within 30 seconds" test is
-- the database's clock_timestamp(). No Go clock reaches a lifetime.

-- ---------------------------------------------------------------------------
-- The authorization code (§4.5, §5.2)
-- ---------------------------------------------------------------------------

-- name: InsertOAuthCode :one
-- The consent POST's one write (§4.5): the code's hash, the app, the approving owner membership,
-- the matched registered redirect URI, the approved scopes (canonical) and the PKCE challenge.
-- created_at and expires_at are the DEFAULTs: exactly 60 seconds by oauth_codes_lifetime.
INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge)
VALUES (@tenant_id, @code_hash, @client_id, @created_by, @redirect_uri, @scopes, @code_challenge)
RETURNING id, expires_at;

-- name: ConsumeOAuthCode :one
-- §5.2 step 3, with the ADR's "Uygulama notları" refinement: the code is spent by ONE conditional
-- UPDATE whose row is taken FOR UPDATE SKIP LOCKED. It answers one row, always:
--   reached = false        the row is locked -- a concurrent exchange of the same code is in flight
--                          -- or gone. The caller answers invalid_grant and touches NO grant: a
--                          concurrent first presentation is not a reuse.
--   reached, spent         this presentation consumed the code (consumed_at = clock_timestamp());
--                          the code's facts come back. The caller COMMITS the consumption whatever
--                          steps 5-6 decide (one shot per code).
--   reached, NOT spent     the row was free and already consumed or expired: a SEQUENTIAL
--                          presentation. If prior_consumed_at and prior_grant_id are set the code
--                          bore a grant -> §5.2 step 4: RevokeOAuthGrant (code_reuse). Otherwise
--                          invalid_grant.
-- Why one statement and not "consume, then look": a loser that looked after the winner committed
-- would see grant_id and revoke the winner's fresh grant (the note's warning). The lock decides
-- once, in the statement that also spends the code. Under READ COMMITTED a row the winner updated
-- and committed before this statement locks it is re-read at its newest version (prior_* are the
-- committed state).
WITH target AS MATERIALIZED (
    SELECT c.id, c.consumed_at, c.grant_id
    FROM oauth_codes AS c
    WHERE c.id = @code_id
      AND c.tenant_id = @tenant_id
    FOR UPDATE SKIP LOCKED
), spent AS (
    UPDATE oauth_codes AS c
    SET consumed_at = clock_timestamp()
    FROM target AS t
    WHERE c.id = t.id
      AND c.tenant_id = @tenant_id
      AND c.consumed_at IS NULL
      AND c.expires_at > clock_timestamp()
    RETURNING c.id, c.client_id, c.redirect_uri, c.code_challenge, c.scopes, c.created_by
)
SELECT (t.id IS NOT NULL)::boolean AS reached,
       (s.id IS NOT NULL)::boolean AS spent,
       t.consumed_at AS prior_consumed_at,
       t.grant_id AS prior_grant_id,
       s.client_id,
       s.redirect_uri,
       s.code_challenge,
       s.scopes,
       s.created_by
FROM (SELECT 1) AS one
LEFT JOIN target AS t ON true
LEFT JOIN spent AS s ON true;

-- name: SetOAuthCodeGrant :execrows
-- §5.2 step 7: the grant the code bore, in the exchange's transaction -- what a later presentation
-- of the spent code revokes. One way (00104's trigger): only a consumed code without a grant.
UPDATE oauth_codes
SET grant_id = @grant_id::uuid
WHERE tenant_id = @tenant_id
  AND id = @code_id
  AND consumed_at IS NOT NULL
  AND grant_id IS NULL;

-- name: DeleteExpiredOAuthCodes :execrows
-- The lazy delete (K-19: this tenant alone, on the request path): at most 100 codes that expired
-- more than 24 hours ago, oldest first, SKIP LOCKED -- the restrictive policy
-- oauth_codes_delete_expired allows no other. A MATERIALIZED batch (T130's measurement in
-- apiidempotency.sql: an IN (... LIMIT ... SKIP LOCKED) rescan deletes past its bound).
WITH doomed AS MATERIALIZED (
    SELECT c.id
    FROM oauth_codes AS c
    WHERE c.tenant_id = @tenant_id
      AND c.expires_at < statement_timestamp() - interval '24 hours'
    ORDER BY c.created_at
    LIMIT 100
    FOR UPDATE SKIP LOCKED)
DELETE FROM oauth_codes AS d
USING doomed
WHERE d.tenant_id = @tenant_id
  AND d.id = doomed.id;

-- ---------------------------------------------------------------------------
-- The grant (§5.2 step 7, §6, §12)
-- ---------------------------------------------------------------------------

-- name: SupersedeLiveOAuthGrants :many
-- §5.2 step 7: the business's live grant to this app, if any, is replaced -- before the new one is
-- inserted, in the same transaction (O-8). Its tokens die with it (the resolver reads the grant's
-- revoked_at). The ids come back for the audit trail. Two exchanges racing for one (business, app)
-- meet at oauth_grants_live_client_key on the INSERT below and the loser rolls back.
UPDATE oauth_grants
SET revoked_at = COALESCE(revoked_at, clock_timestamp()),
    revoked_reason = 'superseded'
WHERE tenant_id = @tenant_id
  AND client_id = @client_id
  AND revoked_at IS NULL
RETURNING id;

-- name: InsertOAuthGrant :one
-- §5.2 step 7: the business's consent, scoped to the code's scopes, created by the approving owner
-- membership. id and created_at are the DEFAULTs.
INSERT INTO oauth_grants (tenant_id, client_id, scopes, created_by)
VALUES (@tenant_id, @client_id, @scopes, @created_by)
RETURNING id, created_at;

-- name: LockLiveOAuthGrant :one
-- §5.3 step 2: the grant's row, locked -- every refresh of one grant runs behind this lock, which is
-- what makes the tolerance (step 4) leave exactly one live pair (§5.3's "Eşzamanlılık kanıtı"). No
-- row: the grant is revoked (invalid_grant). scopes are the grant's: a refresh may narrow the access
-- token below them, never widen it (§5.3).
SELECT id, client_id, scopes
FROM oauth_grants
WHERE tenant_id = @tenant_id
  AND id = @grant_id
  AND revoked_at IS NULL
FOR UPDATE;

-- name: RevokeOAuthGrant :execrows
-- The grant ends, for one of the reasons no person writes: 'client' (the app revoked its refresh
-- token, §6), 'refresh_reuse' (§5.3 step 4, past the 30 seconds), 'code_reuse' (§5.2 step 4). Every
-- token of it stops resolving at once. 0 rows: already revoked (the guard; the trigger is never met).
UPDATE oauth_grants
SET revoked_at = COALESCE(revoked_at, clock_timestamp()),
    revoked_reason = @reason::text
WHERE tenant_id = @tenant_id
  AND id = @id
  AND revoked_at IS NULL;

-- name: DisconnectOAuthGrant :execrows
-- §12, the owner's Disconnect: ADR 0047's statement verbatim -- the reason 'owner' and the
-- membership that pressed it.
UPDATE oauth_grants
SET revoked_at = COALESCE(revoked_at, clock_timestamp()),
    revoked_reason = 'owner',
    revoked_by = @actor::uuid
WHERE id = @id
  AND tenant_id = @tenant_id
  AND revoked_at IS NULL;

-- name: TouchOAuthGrant :execrows
-- §8.1 step 6: last_used_at at most once a minute per grant (TouchAPIKey's rule), the "Connected
-- apps" list's last use. A revoked grant is not touched.
UPDATE oauth_grants
SET last_used_at = clock_timestamp()
WHERE tenant_id = @tenant_id
  AND id = @id
  AND revoked_at IS NULL
  AND (last_used_at IS NULL OR last_used_at <= clock_timestamp() - interval '1 minute');

-- name: ListConnectedApps :many
-- §12: the business's live grants, newest first, each with its approver (the "Approver disabled"
-- pill reads the membership's status -- the grant is the business's, §12) and whether it still has a
-- live refresh token (none: "Expired -- reconnect from the app", the "Uygulama notları"). The app's
-- name and state are resolve_oauth_client's (tappa_app reads nothing of oauth_clients).
SELECT g.id,
       g.client_id,
       g.scopes,
       g.created_by,
       a.full_name AS approver_name,
       a.status AS approver_status,
       g.created_at,
       g.last_used_at,
       EXISTS (SELECT 1
               FROM oauth_tokens AS k
               WHERE k.tenant_id = @tenant_id
                 AND k.grant_id = g.id
                 AND k.kind = 'refresh'
                 AND k.used_at IS NULL
                 AND k.revoked_at IS NULL
                 AND k.expires_at > clock_timestamp())::boolean AS has_live_refresh
FROM oauth_grants AS g
JOIN admin_users AS a ON a.id = g.created_by AND a.tenant_id = g.tenant_id
WHERE g.tenant_id = @tenant_id
  AND g.revoked_at IS NULL
ORDER BY g.created_at DESC, g.id;

-- name: ListLiveWritingOAuthClients :many
-- §11.3 step 2, the invitation queue's gate when neither K-22 column is set: the apps holding a live
-- grant with employees:write in this business -- a grant that still has a live refresh token (a dead
-- grant does not open the gate: the "Uygulama notları"). No LIMIT: the partial UNIQUE (tenant_id,
-- client_id) WHERE revoked_at IS NULL bounds the rows by the registry's size, and a LIMIT without an
-- order could skip the one enabled app (2nd round DÜŞÜK-4). The caller resolves each id with
-- resolve_oauth_client and the first enabled one opens the gate (§11.3 step 3).
SELECT g.client_id
FROM oauth_grants AS g
WHERE g.tenant_id = @tenant_id
  AND g.revoked_at IS NULL
  AND 'employees:write' = ANY (g.scopes)
  AND EXISTS (SELECT 1
              FROM oauth_tokens AS k
              WHERE k.tenant_id = @tenant_id
                AND k.grant_id = g.id
                AND k.kind = 'refresh'
                AND k.used_at IS NULL
                AND k.revoked_at IS NULL
                AND k.expires_at > clock_timestamp())
ORDER BY g.client_id;

-- name: CheckOAuthAccessTokenLive :one
-- /v1's bearer chain (ADR 0047 §8.1 step 4), after resolve_oauth_token_by_hash carried the token's
-- facts: whether the access token is still live by the DATABASE's clock (§14.2's one clock -- the
-- resolver carries expires_at and a Go clock would be a second source). No row: the token is not an
-- access token of this tenant any more (revoked, or lazily deleted) -- 401 invalid_token either way.
SELECT (k.expires_at > clock_timestamp())::boolean AS live
FROM oauth_tokens AS k
WHERE k.tenant_id = @tenant_id
  AND k.id = @token_id
  AND k.kind = 'access'
  AND k.revoked_at IS NULL;

-- name: GetOAuthGrantForPanel :one
-- The owner's Disconnect (§12): the grant's app, read in the session's tenant before the revocation
-- so the trail row names it. No row: not a live grant of this business (another business's id finds
-- none -- RLS and the explicit filter).
SELECT id, client_id
FROM oauth_grants
WHERE tenant_id = @tenant_id
  AND id = @id
  AND revoked_at IS NULL;

-- name: GetTenantAPIGate :one
-- §11.3 step 1, §11.4: K-22's two columns -- GetTenantAPIEnabledAt with api_code. api_enabled_at set:
-- the API is open. api_code set with api_enabled_at NULL: the operator CLOSED it, which overrides a
-- grant. Both NULL: never opened -- the grant decides.
SELECT api_enabled_at, api_code
FROM tenants
WHERE id = @tenant_id;

-- ---------------------------------------------------------------------------
-- The tokens (§5.2 step 7, §5.3, §6)
-- ---------------------------------------------------------------------------

-- name: InsertOAuthToken :one
-- An access token (scopes: its own -- the grant's or a refresh's narrowing) or a refresh token
-- (scopes NULL: it always carries the grant's). expires_at is written HERE from now() and the kind --
-- the text of the statement, never a parameter -- and oauth_tokens_lifetime binds it by equality.
-- expires_in is the token response's (§5.4, the "Uygulama notları": from expires_at, by the
-- database's clock, rounded down -- never a constant 3600).
INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, scopes, expires_at)
VALUES (@tenant_id, @grant_id, @kind::text, @token_hash, sqlc.narg(scopes)::text[],
        now() + CASE @kind::text WHEN 'access' THEN interval '1 hour' ELSE interval '2160 hours' END)
RETURNING id, expires_at, floor(extract(epoch FROM expires_at - clock_timestamp()))::bigint AS expires_in;

-- name: RotateOAuthRefreshToken :one
-- §5.3 step 3, under LockLiveOAuthGrant's lock: the refresh token is used -- by this ONE conditional
-- statement (the `UPDATE tags SET last_ctr ... WHERE last_ctr < $2` shape). A row: normal rotation,
-- mint the new pair in the same transaction. No row: GetOAuthRefreshTokenState says which branch.
UPDATE oauth_tokens
SET used_at = clock_timestamp()
WHERE tenant_id = @tenant_id
  AND id = @token_id
  AND kind = 'refresh'
  AND used_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > clock_timestamp()
RETURNING grant_id;

-- name: GetOAuthRefreshTokenState :one
-- §5.3 steps 4-5, read under the same grant lock after RotateOAuthRefreshToken matched nothing:
-- within_tolerance -- used, not revoked, and its FIRST use (used_at is one way) no more than 30
-- seconds ago by the database's clock -> the tolerance branch (RevokeOtherLiveOAuthTokens, then one
-- new pair); used and past 30 seconds -> reuse (RevokeOAuthGrant 'refresh_reuse'); revoked or
-- expired -> invalid_grant, no grant revocation.
SELECT used_at,
       revoked_at,
       expires_at,
       (used_at IS NOT NULL AND revoked_at IS NULL
        AND clock_timestamp() - used_at <= interval '30 seconds')::boolean AS within_tolerance
FROM oauth_tokens
WHERE tenant_id = @tenant_id
  AND id = @token_id
  AND kind = 'refresh';

-- name: RevokeOtherLiveOAuthTokens :execrows
-- §5.3 step 4, the tolerance, ADR 0047's statement verbatim: every OTHER live token of the grant --
-- every unused refresh token and every access token, whatever its lineage -- is revoked; the USED
-- refresh tokens (the presented one among them) are left alone, so a used token never falls into
-- step 5 and its window keeps counting from its own first use. Then exactly one new pair is minted.
UPDATE oauth_tokens
SET revoked_at = COALESCE(revoked_at, clock_timestamp())
WHERE tenant_id = @tenant_id
  AND grant_id = @grant_id
  AND revoked_at IS NULL
  AND expires_at > clock_timestamp()
  AND NOT (kind = 'refresh' AND used_at IS NOT NULL);

-- name: RevokeOAuthAccessToken :execrows
-- §6: an access token presented to /oauth/revoke ends alone; the grant lives (§18.2 item 13). 0
-- rows: already revoked or not an access token of this tenant -- RFC 7009 answers 200 either way.
UPDATE oauth_tokens
SET revoked_at = COALESCE(revoked_at, clock_timestamp())
WHERE tenant_id = @tenant_id
  AND id = @token_id
  AND kind = 'access'
  AND revoked_at IS NULL;

-- name: DeleteExpiredOAuthTokensForGrant :execrows
-- §5.3's lazy cleanup, in the refresh's transaction, for this grant alone (K-19): access tokens that
-- expired more than an hour ago and refresh tokens past their 2160 hours. The restrictive policy
-- oauth_tokens_delete_expired holds the same bound.
DELETE FROM oauth_tokens
WHERE tenant_id = @tenant_id
  AND grant_id = @grant_id
  AND ((kind = 'access' AND expires_at < statement_timestamp() - interval '1 hour')
       OR (kind = 'refresh' AND expires_at < statement_timestamp()));
