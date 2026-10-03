-- +goose Up

-- Migration 00028 -- M10 WL-1: tenant_branding, the one place a tenant's brand lives.
--
-- WHAT IT HOLDS (ADR 0023 §1, ADR 0024 §4): an accent colour and a logo, nothing else.
-- At most one row per tenant (0..1 against `tenants`), so the scope key IS the primary
-- key. A separate table rather than columns on `tenants`, so that a reader of the
-- tenant row (store.Tenant and the queries over it) does not carry a 256 KiB bytea
-- along with it.
--
-- IT IS NOT EVIDENCE. A brand is changed in place (UPDATE) and reset by setting its
-- fields back to NULL (UPDATE ... NULL); its history is in audit_log, written in the
-- same transaction as the change (WL-4). So there is no append-only trigger and no
-- DELETE: a row, once a tenant has written a brand, stays and may be all-NULL. An
-- all-NULL row and no row both mean "no brand", and the readers (WL-8, WL-9) are to
-- treat them alike.
--
-- RED LINES:
--   section 4.5  Tenant isolation: the five below (tenant_id NOT NULL, a tenant_id-
--                leading index, ENABLE + FORCE RLS, a NULLIF policy with USING and
--                WITH CHECK, an explicit tappa_app grant), plus a composite FK that
--                keeps updated_by inside the row's own tenant.
--   section 4.6  No hard delete. tappa_app holds no DELETE; "remove the logo" is an
--                UPDATE that clears the five logo columns.
--   section 4.1  The logo is stored as bytes and served back as bytes. This migration
--                measures their length and hashes them in two CHECKs below and adds
--                no other code that reads them; ADR 0024 §7 keeps analysis of the image
--                out of the product.


CREATE TABLE tenant_branding (
    -- tenant_id: the scope key AND the primary key (one brand per tenant). The PK is
    -- written as a TABLE constraint below, not as `tenant_id uuid PRIMARY KEY`: both
    -- build the same unique index, but scripts/redline-check.sh R5 does not read the
    -- column spelling as a tenant_id-leading index (measured: it reports "eksik ->
    -- tenant_id ONDE olan indeks"), and it does read the table-constraint spelling.
    tenant_id   uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,

    -- accent: six UPPER-case hex digits, no '#' -- internal/brand.ParseAccent's
    -- canonical spelling and Color.Hex()'s output (ADR 0023 §3). The legibility gate
    -- (brand.Check) is NOT here: it depends on palette constants that live in Go and
    -- may change, so the database stores the shape and the code re-checks the colour on
    -- both the write and the read side (ADR 0023 §3, "Ayni fonksiyon iki tarafta").
    --
    -- ⚠️ char(6), as ADR 0023 §1 writes it, and one property of that type is load-
    -- bearing for the queries: an EXPLICIT cast to char(6) truncates silently
    -- ('1F5C41ZZ'::char(6) = '1F5C41', which then passes this CHECK -- measured), while
    -- an ASSIGNMENT of a longer value raises 22001 when an extra character is not a
    -- space; extra characters that are all trailing spaces are trimmed and the value is
    -- stored canonical ('1F5C41   ' is stored as 1F5C41 -- measured). db/queries/
    -- branding.sql therefore passes the value as text; none of its statements casts to
    -- char(6).
    accent      char(6),

    -- The logo (ADR 0024 §3-§4): the re-encoded bytes internal/brand.Normalize
    -- returns, their type, their pixel size and the lower-case hex sha256 of exactly
    -- those bytes. All five are set together or all five are NULL.
    logo        bytea,
    logo_sha256 text,
    logo_mime   text,
    logo_width  integer,
    logo_height integer,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    -- updated_by: the admin who made the last change. NOT NULL, because a write to this
    -- table is an owner's act in the panel (ADR 0024 §6: owner-only routes); a write
    -- with no author would be one nobody can be asked about.
    updated_by  uuid NOT NULL,

    CONSTRAINT tenant_branding_pkey PRIMARY KEY (tenant_id),

    CONSTRAINT tenant_branding_accent_canonical
        CHECK (accent ~ '^[0-9A-F]{6}$'),

    -- 262 144 = 256 KiB, internal/brand.LogoMaxOutputBytes. The lower bound is not in
    -- ADR 0024 §4: an empty logo would be served as an empty image under an immutable
    -- cache header, and Normalize checks that its output decodes (ADR 0024 §3), which
    -- an empty byte string does not.
    CONSTRAINT tenant_branding_logo_size
        CHECK (octet_length(logo) BETWEEN 1 AND 262144),

    -- Two checks on the digest, and their NAMES are ordered on purpose: PostgreSQL
    -- tests CHECK constraints in alphabetical order of name, so a wrongly spelled digest
    -- is reported by its shape (`_hex`) before the comparison (`_matches_logo`) runs
    -- (measured: declaring `_matches_logo` first still reports an upper-case digest as
    -- `_hex`).
    --
    -- _matches_logo is not in ADR 0024 §4 either, and it is what makes "sha256-
    -- addressed" a property of the row instead of a promise of the writer: the logo
    -- is served at /t/logo/{sha} and /admin/brand/logo/{sha} with `immutable` caching
    -- (ADR 0024 §5), so a row whose digest named other bytes would leave the wrong
    -- picture cached as immutable in a browser that fetched it. sha256(bytea) and
    -- encode(bytea, text) are both IMMUTABLE (pg_proc.provolatile = 'i', measured),
    -- which is what PostgreSQL assumes of a CHECK's condition.
    CONSTRAINT tenant_branding_logo_sha256_hex
        CHECK (logo_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT tenant_branding_logo_sha256_matches_logo
        CHECK (logo_sha256 = encode(sha256(logo), 'hex')),

    -- Closed set: the two types Normalize writes (ADR 0024 §1). The stored value is
    -- served as the Content-Type without sniffing (ADR 0024 §5), so the CHECK keeps it
    -- to these two exact strings.
    CONSTRAINT tenant_branding_logo_mime_known
        CHECK (logo_mime IN ('image/png', 'image/jpeg')),

    -- 1..512: the long edge after the box filter is at most 512 and the short edge is
    -- rounded to at least one pixel (internal/brand.logoTargetSize).
    CONSTRAINT tenant_branding_logo_width_range
        CHECK (logo_width BETWEEN 1 AND 512),
    CONSTRAINT tenant_branding_logo_height_range
        CHECK (logo_height BETWEEN 1 AND 512),

    -- All five logo columns or none. Without it a half-written logo -- bytes without a
    -- type, a digest without bytes -- would reach a page that renders <img> from
    -- whichever field it reads first.
    CONSTRAINT tenant_branding_logo_all_or_none
        CHECK (num_nulls(logo, logo_sha256, logo_mime, logo_width, logo_height) IN (0, 5)),

    -- Same-tenant composite FK, the M1-03 pattern (admin_sessions, password_resets,
    -- billing_periods): the admin who changed the brand must belong to the tenant whose
    -- brand it is. admin_users_id_tenant_key (00006) exists so this reference can be
    -- made. A single-column FK on updated_by alone would accept ANOTHER tenant's admin
    -- id (FK checks do not see RLS) and would answer "does this admin id exist
    -- anywhere" to whoever tried one.
    CONSTRAINT tenant_branding_updated_by_fk
        FOREIGN KEY (updated_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT
);

-- NO INDEX BEYOND THE PRIMARY KEY, and in particular NO UNIQUE ON logo_sha256.
-- Two tenants that upload the same picture store the same bytes and the same digest; a
-- global unique would refuse the second and so tell it the picture already exists in
-- some other tenant (ADR 0024 Iddia D, WL-3 devri). The tenant-scoped alternative,
-- (tenant_id, logo_sha256), is already implied by the primary key: one row per tenant
-- holds at most one digest. The reads in db/queries/branding.sql are by tenant_id, the
-- PK lookup.

ALTER TABLE tenant_branding ENABLE ROW LEVEL SECURITY;
-- FORCE: the table owner is subject to the policy too (superuser excepted -- M0-03).
ALTER TABLE tenant_branding FORCE ROW LEVEL SECURITY;

-- The policy expression is NORMATIVE and verbatim per ADR 0002 madde 3 / Q27: with no
-- context the GUC is either NULL (never written) or '' (written once, tx over); NULLIF
-- collapses both to NULL so no row matches (fail-closed). A bare ::uuid cast raises on
-- the empty string and makes behaviour depend on the connection's history.
CREATE POLICY tenant_branding_tenant_isolation ON tenant_branding
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- PRIVILEGES: everything is revoked first, then granted back by name. The default ACL
-- for new tables is `ar` on a fresh cluster (scripts/db-init/01-roles.sql) but `arwd`
-- on the development database (measured: pg_default_acl = {tappa_app=arwd/
-- tappa_owner}) and, per 01-roles.sql's note (b), after a pg_dump restore. REVOKE ALL
-- first makes this block's result independent of which one is in force.
--
--   SELECT  table-level. GetTenantLogo reads the bytes; GetTenantBrand does not
--           (db/queries/branding.sql), and that difference is a query's, not a grant's.
--   INSERT  (tenant_id, updated_by) only -- EnsureTenantBrand creates an empty row and
--           the brand fields arrive by UPDATE. created_at and updated_at take their
--           defaults; tappa_app holds no INSERT on them.
--   UPDATE  the brand fields and the two who/when columns. NOT tenant_id (tappa_app
--           does not move a row between tenants; with a table-level UPDATE the
--           policy's WITH CHECK refuses the move too -- measured) and NOT created_at.
--   DELETE  none (section 4.6). TRUNCATE, REFERENCES, TRIGGER: none.
REVOKE ALL ON tenant_branding FROM tappa_app;
GRANT SELECT ON tenant_branding TO tappa_app;
GRANT INSERT (tenant_id, updated_by) ON tenant_branding TO tappa_app;
GRANT UPDATE (accent, logo, logo_sha256, logo_mime, logo_width, logo_height,
              updated_at, updated_by) ON tenant_branding TO tappa_app;


-- +goose Down

-- DROP TABLE takes the policy, RLS flags, grants, constraints, the PK index and both
-- foreign keys with it -- including the foreign keys' internal RI triggers (measured
-- on dev: 4 on tenant_branding, 2 on tenants, 2 on admin_users; the schema dump after
-- Down equals v27's). The Up section creates no function, no role, no user trigger
-- and no sequence (the table has no serial column).
DROP TABLE tenant_branding;
