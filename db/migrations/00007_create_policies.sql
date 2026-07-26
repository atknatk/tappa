-- +goose Up

-- Migration 0007 -- Policy engine schema (M3-02). Three tables holding the
-- CUSTOMER-authored policy layer of the decision engine (internal/policy):
--   policies           : a named policy belonging to the baseline or tenant layer.
--   policy_versions    : the versioned IAM-style document of a policy. APPEND-ONLY.
--   policy_attachments : binds a policy to a resource (location/department/employee/*).
--
-- WHAT IS DELIBERATELY ABSENT -- GUARDRAILS (CLAUDE.md section 5 lines 1-5, sys:*):
-- the guardrail layer is compiled INTO the code (internal/policy, M3-05); it is NOT
-- stored here and there is NO table that can hold it. If a guardrail could be
-- written to the DB, a single SQL write (or a compromised app role) could disable a
-- section 4 red line. The `layer` CHECK below accepts only 'baseline'/'tenant' so
-- 'guardrail' cannot even be spelled into a row -- the absence is STRUCTURAL, not a
-- convention (M3-02 card; ADR 0004).
--
-- KIRMIZI CIZGILER:
--   section 4.3  policy_versions is APPEND-ONLY -- a policy's history is an evidence
--                chain (each transaction records the policy_version_id that decided
--                it, M3-07), so editing a version would rewrite why a past,
--                immutable record was flagged. Belt+braces like transactions/
--                audit_log: REVOKE UPDATE, DELETE (privilege) + a BEFORE UPDATE OR
--                DELETE trigger (stops even the superuser owner). Reuses the shared
--                tappa_forbid_mutation() defined in migration 0005.
--   section 4.5  Tenant isolation: every table gets the RLS five + explicit GRANT,
--                and cross-tenant links are closed STRUCTURALLY by composite
--                same-tenant FKs (policy_id, tenant_id) -> policies (id, tenant_id).
--   section 4.6  No hard delete of a policy: enabled=false is the OFF switch, never
--                a DELETE -- a disabled policy keeps its version history / audit
--                trail (prefer a status field over deletion).


-- --- policies ----------------------------------------------------------------
-- A named policy. It is a CONTAINER; its actual rules live in policy_versions (the
-- document). policies is MUTABLE (NOT append-only): the enabled toggle and name
-- edits are ordinary UPDATEs. Cross-tenant writes are stopped by RLS WITH CHECK;
-- hard delete is revoked (section 4.6 -- disable, do not delete).
CREATE TABLE policies (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: mandatory scope key on every table (section 4.5).
    -- ON DELETE RESTRICT: a tenant with policies cannot be deleted (audit trail).
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    name       text NOT NULL,
    -- layer: WHICH of the two DB-stored layers this policy belongs to. CLOSED
    -- vocabulary; 'guardrail' is INTENTIONALLY not allowed (guardrails are compiled
    -- into internal/policy, never stored -- see the header). This CHECK is the
    -- structural guarantee that a section 4 red line cannot be authored as data.
    layer      text NOT NULL CHECK (layer IN ('baseline', 'tenant')),
    -- enabled: the tenant's ON/OFF switch (M3-02 card). Disabling is NOT deleting
    -- (section 4.6) -- a disabled policy is inert but keeps its version history.
    -- NOT NULL DEFAULT true: a freshly created policy is active unless turned off.
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- created_by: the admin who created this policy, OR NULL when Tappa's system
    -- provisioning creates a baseline policy (M3-06/M7-03 -- no human actor). FK-LESS
    -- uuid ON PURPOSE: the admin identity FK (-> admin_users) is DEFERRED to M6/M7,
    -- exactly like transactions.entered_by and transaction_reviews.reviewer_id
    -- (M1-11 back-FK deferral, state.md). A composite same-tenant FK would also
    -- require created_by NOT NULL, which the baseline (system-authored) case forbids.
    created_by uuid,

    -- Composite same-tenant UNIQUE, REQUIRED so policy_versions/policy_attachments
    -- can reference (id, tenant_id): a composite FK needs a UNIQUE/PK on its target
    -- columns. id is already the global PK so (id, tenant_id) is trivially unique;
    -- this constraint exists ONLY to enable the structural cross-tenant block below
    -- (00005/00006 pattern).
    CONSTRAINT policies_id_tenant_key UNIQUE (id, tenant_id)
);

-- tenant_id-FIRST index (section 6 / R5): RLS and tenant-scoped listing. The
-- composite UNIQUE above is id-first so it does NOT satisfy R5 -- this separate
-- index is REQUIRED (same reasoning as admin_users).
CREATE INDEX policies_tenant_idx ON policies (tenant_id);

ALTER TABLE policies ENABLE ROW LEVEL SECURITY;
-- FORCE: even the table owner is subject to the policy (except the superuser -- M0-03).
ALTER TABLE policies FORCE ROW LEVEL SECURITY;

-- Policy expression is NORMATIVE per ADR 0002 madde 3 / Q27: with no context the
-- GUC is either NULL (never written) or '' (written once, tx ended); NULLIF maps
-- both to NULL -> no row matches (fail-closed). A bare ::uuid cast is FORBIDDEN: it
-- raises on the empty string, making behavior depend on connection history.
CREATE POLICY policies_tenant_isolation ON policies
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- DELETE is deliberately NOT granted (section 4.6): a policy is disabled
-- (enabled=false), never hard-deleted -- its version history is an evidence chain.
-- DANGER (M1-04 lesson): db-init's ALTER DEFAULT PRIVILEGES grants tappa_app all
-- four DMLs on every new table; removing DELETE from GRANT alone is NOT enough --
-- it must be explicitly REVOKEd. UPDATE stays (the enabled toggle IS an UPDATE).
GRANT SELECT, INSERT, UPDATE ON policies TO tappa_app;
REVOKE DELETE ON policies FROM tappa_app;


-- --- policy_versions ---------------------------------------------------------
-- The versioned document of a policy. APPEND-ONLY (section 4.3): editing a rule
-- means writing a NEW version, never mutating an existing one -- because each
-- transaction records the exact policy_version_id that decided it (M3-07), so a
-- mutated version would silently rewrite the explanation of past, immutable
-- records. Same belt+braces as transactions: REVOKE + trigger.
CREATE TABLE policy_versions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- policy_id NOT NULL: a version always belongs to a policy. The composite FK
    -- (policy_id, tenant_id) -> policies (id, tenant_id) (both NOT NULL -> ALWAYS
    -- checked) forces the parent policy to be in the SAME tenant (cross-tenant
    -- block). ON DELETE RESTRICT: a policy with versions cannot be deleted.
    policy_id  uuid NOT NULL,
    -- version_no: monotonic per policy, assigned by the app (M3-03). >= 1: versions
    -- start at 1. Uniqueness is per (tenant, policy) -- see the constraint below.
    version_no integer NOT NULL CHECK (version_no >= 1),
    -- document: the IAM-style policy document (statements/effect/action/condition).
    -- jsonb and FREE-FORM at the DB layer (M3-02 card): schema validation happens in
    -- the application at WRITE time (M3-03), NOT here -- this keeps forward-version
    -- compatibility (a newer document shape must not require a migration). NOT NULL:
    -- a version with no document is meaningless. No DEFAULT -- the author supplies it.
    document   jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- created_by: the admin who authored this version, or NULL for a system-authored
    -- baseline version. FK-LESS uuid -- admin FK deferred to M6/M7 (see policies).
    created_by uuid,

    -- version_no is monotonic PER POLICY -> UNIQUE(tenant_id, policy_id, version_no).
    -- tenant_id FIRST, so this UNIQUE ALSO provides the tenant-first index R5 requires
    -- (section 6) and serves "get the latest version of a policy"
    -- (WHERE tenant_id=? AND policy_id=? ORDER BY version_no DESC) -- no separate
    -- index needed. (00006 reasons the same way about whether a UNIQUE meets R5.)
    CONSTRAINT policy_versions_no_key UNIQUE (tenant_id, policy_id, version_no),
    CONSTRAINT policy_versions_policy_fk
        FOREIGN KEY (policy_id, tenant_id)
        REFERENCES policies (id, tenant_id) ON DELETE RESTRICT
);

ALTER TABLE policy_versions ENABLE ROW LEVEL SECURITY;
-- FORCE: even the table owner is subject to the policy (except the superuser -- M0-03).
ALTER TABLE policy_versions FORCE ROW LEVEL SECURITY;

CREATE POLICY policy_versions_tenant_isolation ON policy_versions
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- APPEND-ONLY (section 4.3) -- BELT 1: privilege. tappa_app may only read+append.
-- REVOKE is REQUIRED (M1-04 lesson: removing UPDATE/DELETE from GRANT does not undo
-- the ALTER DEFAULT PRIVILEGES grant). Verified with has_table_privilege.
GRANT SELECT, INSERT ON policy_versions TO tappa_app;
REVOKE UPDATE, DELETE ON policy_versions FROM tappa_app;

-- APPEND-ONLY -- BELT 2: trigger. Closes the bypass path (superuser tappa_owner,
-- which ignores both RLS and privileges). Reuses tappa_forbid_mutation() defined in
-- migration 0005 -- the SAME shared function; it is NOT redefined here and MUST NOT
-- be dropped in Down (transactions/audit_log/transaction_reviews still use it).
CREATE TRIGGER policy_versions_no_mutation
    BEFORE UPDATE OR DELETE ON policy_versions
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();


-- --- policy_attachments ------------------------------------------------------
-- Binds a policy to a RESOURCE it applies to: 'location/<id>', 'department/<id>',
-- 'employee/<id>', or '*' (M3 model -- resources). The 9 branches of a chain need
-- not share one policy (Rusty Bar night shift vs HQ office). MUTABLE: attachments
-- can be added/removed as scope changes (no append-only requirement -- an
-- attachment carries no decision history of its own; past explanations are pinned
-- by transactions.policy_version_id + policy_context, M3-07).
CREATE TABLE policy_attachments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    -- policy_id NOT NULL: an attachment always binds a policy. Composite same-tenant
    -- FK (both NOT NULL -> ALWAYS checked) -> parent policy is same-tenant.
    policy_id  uuid NOT NULL,
    -- resource: the scope target. FREE-FORM text; the resource grammar
    -- (location/<id>, ..., *) is validated in the application (M3-03/M3-04), not by a
    -- DB CHECK -- the resource vocabulary grows with the model and a CHECK would
    -- force a migration per new resource kind. NOT NULL: an attachment must scope to
    -- something.
    resource   text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    -- One policy attaches to a given resource at most ONCE (a duplicate binding is
    -- meaningless). tenant_id FIRST, so this UNIQUE also provides the tenant-first
    -- index R5 requires and serves "list attachments of a policy"
    -- (WHERE tenant_id=? AND policy_id=?) -- no separate index needed.
    CONSTRAINT policy_attachments_resource_key UNIQUE (tenant_id, policy_id, resource),
    CONSTRAINT policy_attachments_policy_fk
        FOREIGN KEY (policy_id, tenant_id)
        REFERENCES policies (id, tenant_id) ON DELETE RESTRICT
);

ALTER TABLE policy_attachments ENABLE ROW LEVEL SECURITY;
-- FORCE: even the table owner is subject to the policy (except the superuser -- M0-03).
ALTER TABLE policy_attachments FORCE ROW LEVEL SECURITY;

CREATE POLICY policy_attachments_tenant_isolation ON policy_attachments
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- policy_attachments is fully mutable (add/remove bindings): all four DMLs. No
-- append-only requirement, so no REVOKE (unlike policy_versions). Explicit GRANT per
-- section 6 / R5.
GRANT SELECT, INSERT, UPDATE, DELETE ON policy_attachments TO tappa_app;


-- +goose Down

-- FK order: children (policy_versions, policy_attachments -> policies) first, then
-- policies. DROP TABLE also drops each table's policy, RLS settings, GRANTs,
-- indexes, constraints and TRIGGERS. The shared tappa_forbid_mutation() function is
-- NOT dropped here -- it is owned by migration 0005 and still used by
-- transactions/audit_log/transaction_reviews. No roles, no other functions.
DROP TABLE policy_attachments;
DROP TABLE policy_versions;
DROP TABLE policies;
