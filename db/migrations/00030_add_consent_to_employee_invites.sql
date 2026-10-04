-- +goose Up
-- ADR 0025: activation completes on a physical NFC tap, not on the consent form.
--
-- The wizard's consent POST no longer spends the invitation. It RECORDS the
-- consent on the invitation row and binds it to the one browser that gave it;
-- the first genuine NFC tap from that browser then consumes the invitation and
-- issues the session (ConsumeInviteAndActivate, which now REQUIRES both columns).
--
-- consented_at: when the employee confirmed the GDPR Art. 13 notice. Durable and
-- server-side, so "did this person consent before being activated" is answered
-- by the row that was consumed, not by a log line. Re-consenting (the link opened
-- again in another browser) moves it forward together with the binding below.
--
-- consent_binding_hash: the HMAC (internal/invite, its own domain label) of a
-- random token that lives ONLY in the consenting browser's HttpOnly activation
-- cookie. It is what stops a planted cookie from completing an activation: a
-- cross-site GET can plant a CODE in a victim's browser, but not a binding,
-- because a binding is minted only by the CSRF-protected consent POST. Same shape
-- CHECK as code_hash (00009) for the same reason: the column must not be able to
-- hold the raw token.
ALTER TABLE employee_invites ADD COLUMN consented_at timestamptz;
ALTER TABLE employee_invites ADD COLUMN consent_binding_hash text
    CONSTRAINT employee_invites_consent_binding_hash_shape
    CHECK (consent_binding_hash ~ '^[0-9a-f]{64}$');

-- The two columns are one fact: a consent without a binding cannot be completed
-- by anyone, and a binding without a consent would be a credential nobody agreed
-- to. Either both or neither.
ALTER TABLE employee_invites ADD CONSTRAINT employee_invites_consent_pair
    CHECK ((consented_at IS NULL) = (consent_binding_hash IS NULL));

COMMENT ON COLUMN employee_invites.consented_at IS
    'GDPR Art. 13 consent recorded by the activation wizard (ADR 0025). '
    'Activation (used_at) requires it; it never implies activation by itself.';
COMMENT ON COLUMN employee_invites.consent_binding_hash IS
    'HMAC of the token held by the consenting browser (ADR 0025). Never the raw token.';

-- Column-level UPDATE only, extending 00012's grant. tappa_resolver is NOT
-- granted either column: resolve_invite_by_code_hash is unchanged, and consent
-- is read inside the tenant context like every other tenant-scoped fact.
GRANT UPDATE (used_at, cancelled_at, consented_at, consent_binding_hash)
    ON employee_invites TO tappa_app;

-- +goose Down
-- A table-level REVOKE also revokes every column-level grant (PostgreSQL docs,
-- REVOKE), so the narrower 00012 grant is restored explicitly afterwards.
REVOKE UPDATE ON employee_invites FROM tappa_app;
GRANT UPDATE (used_at, cancelled_at) ON employee_invites TO tappa_app;
ALTER TABLE employee_invites DROP CONSTRAINT employee_invites_consent_pair;
ALTER TABLE employee_invites DROP COLUMN consent_binding_hash;
ALTER TABLE employee_invites DROP COLUMN consented_at;
