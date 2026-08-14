-- The four LEGAL TEXTS (M7-06, migration 00020).
--
-- 🔴 NEITHER QUERY NAMES A TENANT, AND THAT IS THE STRUCTURAL HALF OF section 4.5
-- HERE. legal_documents carries no tenant_id (see 00020 for why), so there is no
-- column to filter on and no tenant id to pass -- which means the generated
-- *Params types below have no TenantID field, and a cross-tenant read cannot be
-- expressed on this path even by mistake. internal/handler's
-- TestLegalStore_CannotNameATenantAtAll asserts that over the GENERATED types
-- rather than over this file, so the property survives a hand edit here.
--
-- The exemption does not extend to the AUDIT TRAIL: a publication is recorded in
-- audit_log, whose tenant_id is NOT NULL, under the tenant of the admin who
-- published it -- their own, the same one every other panel write uses.

-- name: PublishLegalDocument :one
-- Appends one version. There is no UPDATE anywhere in this file and there cannot
-- be one: 00020 revokes UPDATE and DELETE from tappa_app and binds the table owner
-- with the 0005 trigger, so a correction is a NEW ROW (section 4.3's remedy).
-- ⚠️ published_by IS WRITTEN AND NEVER RETURNED, and that is a privilege as well as
-- a choice: 00020 grants tappa_app INSERT on that column and NO SELECT on it, so a
-- RETURNING list naming it would fail at run time. The reason is a cross-tenant
-- oracle — the table has no tenant scope, so a readable admin uuid is a fact about
-- somebody else's business that any tenant's connection could fetch.
INSERT INTO legal_documents (slug, body, published_by)
VALUES (@slug, @body, sqlc.narg('published_by'))
RETURNING id, slug, body, published_at;

-- name: ListPublishedLegalDocuments :many
-- The CURRENT text of every document that has one -- at most one row per slug, the
-- most recently published. published_by is NOT selected; see PublishLegalDocument.
--
-- DISTINCT ON rather than a window function or a correlated subquery: it walks
-- legal_documents_slug_published_idx (slug, published_at DESC) and stops at the
-- first row of each slug group, so the cost is bounded by the number of DISTINCT
-- SLUGS (at most four, by the CHECK on the column) and not by the number of
-- versions ever published.
--
-- ⚠️ id DESC IS A DETERMINISTIC TIEBREAK, NOT A "LATER" ONE, and the difference is
-- written down so the next reader does not mistake it for one. published_at
-- defaults to now(), which is TRANSACTION time, so two versions of the SAME slug
-- appended inside ONE transaction would carry the identical timestamp; a uuid v4
-- carries no time, so id DESC cannot say which of those two is newer. What it does
-- guarantee is that every read resolves the tie the SAME WAY -- a legal document
-- that alternates between two texts depending on the executor's mood is the worse
-- failure. The case is not reachable today: the operator screen appends at most one
-- version per slug per request, so within a transaction the slugs are distinct.
SELECT DISTINCT ON (slug) id, slug, body, published_at
FROM legal_documents
ORDER BY slug, published_at DESC, id DESC;
