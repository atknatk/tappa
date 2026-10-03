-- branding.sql -- the tenant's brand: an accent and a logo (M10 WL-1; ADR 0023 §1,
-- ADR 0024 §4). Table: tenant_branding, migration 00028.
--
-- TENANT SCOPE (CLAUDE.md section 4.5, belt + braces on RLS): the eight statements in
-- this file name @tenant_id explicitly (the WHERE of the seven SELECT/UPDATE is pinned
-- by TestTenantBranding_EveryStatementNamesTheTenant) and are meant to run inside
-- db.(*DB).WithTenant, with the tenant taken from the verified session and not from
-- the request (ADR 0024 §5).
--
-- TWO READS, AND THE DIFFERENCE BETWEEN THEM IS THE POINT. GetTenantBrand is the
-- per-page read (tap screen, panel chrome, editor) and does NOT select `logo`: the
-- bytes are up to 256 KiB and a page needs their digest and size to render an <img>,
-- not the bytes. Of the statements in this file, GetTenantLogo is the one that returns
-- the bytes; it is for the two logo routes (WL-6). A test reads the generated statement
-- text of the per-page reads (internal/db/branding_test.go).
--
-- HOW A WRITE IS MEANT TO RUN (WL-4: the change and its audit_log row in ONE
-- transaction, with the value it replaced). Inside one WithTenant:
--
--   1. EnsureTenantBrand        -- the row exists from here on (a no-op if it did)
--   2. GetTenantBrandForUpdate  -- locks it and returns the value being replaced
--   3. SetTenantAccent / SetTenantLogo / ClearTenantAccent / ClearTenantLogo
--   4. the audit row (internal/audit, Recorder.RecordTx)
--
-- Steps 1 and 2 together are why the "before" value is the stored one when two owners
-- save at the same time. Without step 1, two FIRST saves would both read "no row" in
-- step 2 and the second audit row would record a NULL "before" that was never stored.
-- With it, the second transaction waits -- in step 1 on the first one's uncommitted
-- insert when the row is new, in step 2 on its row lock when the row exists -- and
-- step 2 then returns what the first one committed. Measured for those two orders:
-- TestTenantBranding_EnsureThenLockReturnsWhatTheOtherWriterCommitted.
--
-- NO DELETE. A reset is an UPDATE that sets the field back to NULL (ADR 0023 §1); the
-- application role holds no DELETE on this table (migration 00028).

-- name: GetTenantBrand :one
-- The per-page read. Everything a page needs to draw the brand, and not the logo's
-- bytes. A row whose fields are all NULL means "no brand", exactly like no row.
SELECT accent, logo_sha256, logo_mime, logo_width, logo_height, updated_at, updated_by
FROM tenant_branding
WHERE tenant_id = @tenant_id;

-- name: GetTenantBrandForUpdate :one
-- Step 2 of a write: the same columns as GetTenantBrand, row-locked until the
-- transaction ends, so the audit row's "before" is the value this write replaces.
-- Call EnsureTenantBrand first in the same transaction (see the file header).
SELECT accent, logo_sha256, logo_mime, logo_width, logo_height, updated_at, updated_by
FROM tenant_branding
WHERE tenant_id = @tenant_id
FOR UPDATE;

-- name: GetTenantLogo :one
-- The logo's bytes and the type they are served as, for the logo routes (WL-6).
-- Addressed by tenant AND digest: another tenant's digest and a digest nobody stored
-- both find no row -- the same pgx.ErrNoRows, which WL-6 turns into the same 404
-- (ADR 0024 §5, Iddia D).
SELECT logo, logo_mime
FROM tenant_branding
WHERE tenant_id = @tenant_id
  AND logo_sha256 = sqlc.arg(logo_sha256)::text;

-- name: EnsureTenantBrand :exec
-- Step 1 of a write: creates the tenant's empty row if it has none. Only tenant_id and
-- updated_by are written; created_at and updated_at take their defaults and every
-- brand field stays NULL until step 3.
INSERT INTO tenant_branding (tenant_id, updated_by)
VALUES (@tenant_id, @updated_by)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: SetTenantAccent :execrows
-- Saves the accent. @accent is the canonical six upper-case hex digits
-- (internal/brand.Color.Hex); the CHECK `^[0-9A-F]{6}$` and the column length refuse
-- the nine other spellings measured in TestTenantBranding_ChecksRefuseHostileValues.
-- It is passed as text on purpose: an explicit ::char(6) cast would TRUNCATE a longer
-- value silently, whereas assigning text to the char(6) column raises 22001 when an
-- extra character is not a space. Extra characters that are all trailing spaces are
-- trimmed: '1F5C41   ' is accepted and stored as 1F5C41 (migration 00028; all three
-- measured). The stored value can therefore differ from the argument's text; read it
-- back, or pass Color.Hex(), where the exact value matters (an audit row's "after").
-- Returns the number of rows changed: 1, or 0 when step 1 was skipped.
UPDATE tenant_branding
SET accent     = sqlc.arg(accent)::text,
    updated_at = now(),
    updated_by = @updated_by
WHERE tenant_id = @tenant_id;

-- name: ClearTenantAccent :execrows
-- Resets the accent: the button goes back to the default colour. An UPDATE, not a
-- DELETE (section 4.6; the row may still carry a logo).
UPDATE tenant_branding
SET accent     = NULL,
    updated_at = now(),
    updated_by = @updated_by
WHERE tenant_id = @tenant_id;

-- name: SetTenantLogo :execrows
-- Saves a logo: internal/brand.Logo's Data, SHA256, MIME, Width and Height. All five
-- together -- the table refuses a partial logo, a digest that is not the sha256 of
-- the bytes, a type outside image/png and image/jpeg, a size outside 1..512 and more
-- than 262 144 bytes (migration 00028).
UPDATE tenant_branding
SET logo        = sqlc.arg(logo)::bytea,
    logo_sha256 = sqlc.arg(logo_sha256)::text,
    logo_mime   = sqlc.arg(logo_mime)::text,
    logo_width  = sqlc.arg(logo_width)::integer,
    logo_height = sqlc.arg(logo_height)::integer,
    updated_at  = now(),
    updated_by  = @updated_by
WHERE tenant_id = @tenant_id;

-- name: ClearTenantLogo :execrows
-- Removes the logo: all five logo columns back to NULL in one statement. An UPDATE,
-- not a DELETE (section 4.6; the row may still carry an accent).
UPDATE tenant_branding
SET logo        = NULL,
    logo_sha256 = NULL,
    logo_mime   = NULL,
    logo_width  = NULL,
    logo_height = NULL,
    updated_at  = now(),
    updated_by  = @updated_by
WHERE tenant_id = @tenant_id;
