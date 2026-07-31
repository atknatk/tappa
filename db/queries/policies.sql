-- policies.sql -- reading a tenant's stored policy layer, and MATERIALISING the
-- Tappa-managed baseline into it.
--
-- WHY THESE QUERIES EXIST AT ALL, measured before they were written. The
-- decision engine's rows 6 and 7 of CLAUDE.md section 5 -- the ordinary `ok` and
-- the ordinary `flag`, i.e. the MAIN PATH of the whole product -- are BASELINE
-- decisions, and migration 00008 requires a baseline decision to name a real
-- policy_versions row: the consistency CHECK demands policy_version_id IS NOT
-- NULL for layer 'baseline', and the composite FK demands it resolve to a
-- same-tenant version. Measured against the dev database, as tappa_app, for the
-- seeded Kebab Factory tenant:
--
--   policies = 0, policy_versions = 0
--   INSERT ... policy_layer='baseline', policy_version_id=<any uuid>
--     -> ERROR 23503 transactions_policy_version_fk
--
-- So before this file existed, an ordinary approved check-in COULD NOT BE
-- WRITTEN. That is a section 4.6 record loss on the most common path there is,
-- and it is not hypothetical: M7-03 (provisioning) is the task that was supposed
-- to write these rows and it has not been done.
--
-- THE FIX IS TO MATERIALISE, NOT TO SKIP. The alternative -- evaluate with no
-- baseline -- writes the record but sends EVERY tap to the fail-to-review default,
-- which is a silent, permanent degradation dressed as safety. So the tap path
-- materialises the baseline the first time it needs it, idempotently, and M7-03
-- takes the job over at provisioning time (see internal/domain/checkin).
--
-- IDEMPOTENCY IS ANCHORED ON CONSTRAINTS THAT ALREADY EXIST, so no migration was
-- needed -- but the three statements below do NOT all use the same one, and an
-- earlier version of this comment said "the primary key" for all three, which was
-- wrong for two of them:
--
--   policies            ON CONFLICT (id)                       -- the PRIMARY KEY
--   policy_versions     ON CONFLICT ON CONSTRAINT
--                       policy_versions_no_key                 -- UNIQUE (tenant_id, policy_id, version_no)
--   policy_attachments  ON CONFLICT ON CONSTRAINT
--                       policy_attachments_resource_key        -- UNIQUE (tenant_id, policy_id, resource)
--
-- policies has no (tenant_id, name) key, which is why its id is DERIVED from
-- (tenant, baseline document name) with uuid v5: that makes its primary key a
-- usable conflict target. The other two already had a natural key to conflict on.
-- All three keep this an INSERT-ONLY change to append-only data: policy_versions
-- is append-only by privilege AND by trigger (00007), so "write it again" must be
-- a no-op rather than a second row.
--
-- TENANT SCOPE (section 4.5, belt + braces): every statement carries an explicit
-- tenant_id and runs inside db.(*DB).WithTenant, whose RLS WITH CHECK refuses a
-- row for any other tenant.

-- name: ListPolicySet :many
-- The tenant's stored policy layer: one row per policy, carrying its LATEST
-- version's id and document.
--
-- DISTINCT ON (p.id) with ORDER BY p.id, v.version_no DESC takes the highest
-- version_no per policy -- "latest" is the version number, never created_at,
-- because version_no is the monotonic sequence 00007 defines and two versions can
-- share a timestamp. A policy with no version yet is NOT returned (an inner
-- join): a policy without a document decides nothing, and returning it would let
-- a caller pin a policy_version_id it does not have.
--
-- enabled IS RETURNED, NOT FILTERED. The caller needs to tell "this tenant turned
-- this baseline statement OFF" (leave it out of the Set) apart from "this baseline
-- statement was never materialised" (materialise it) -- a WHERE enabled would make
-- those two indistinguishable and turn a deliberate opt-out into a permanent
-- re-provisioning loop. Disabling is the tenant's off switch and is never a
-- delete (section 4.6, 00007).
--
-- ROW ORDER IS NOT THE EVALUATION ORDER. This comes back ordered by a random
-- uuid; the evaluator's tiebreak needs baseline-in-document-order, so the caller
-- re-orders against the canonical list in internal/policy. Relying on this order
-- would make which sid gets reported depend on uuid generation.
SELECT DISTINCT ON (p.id)
       p.id AS policy_id, p.name, p.layer, p.enabled,
       v.id AS version_id, v.version_no, v.document
FROM policies p
JOIN policy_versions v
  ON v.tenant_id = p.tenant_id
 AND v.policy_id = p.id
WHERE p.tenant_id = @tenant_id
ORDER BY p.id, v.version_no DESC;

-- name: EnsureBaselinePolicy :exec
-- Materialise one managed-baseline policy CONTAINER. layer is fixed to 'baseline'
-- in the statement rather than taken as a parameter: this query exists for the
-- Tappa-authored baseline only, and a caller that could pass 'tenant' would be
-- able to author a tenant policy from the tap path. ('guardrail' is not spellable
-- at all -- 00007's CHECK sees to that, which is what keeps a red line out of the
-- database.)
--
-- created_by is NULL: the author is Tappa's system provisioning, not a human
-- admin (00007 documents exactly this case).
--
-- ON CONFLICT (id) DO NOTHING is what makes a second tap free and a hundred
-- concurrent first taps produce one row. It also means an EXISTING row is left
-- exactly as it is -- including enabled=false. A tenant that switched a baseline
-- statement off must not have it switched back on by someone walking up to a
-- plaque.
INSERT INTO policies (id, tenant_id, name, layer, enabled, created_by)
VALUES (@id, @tenant_id, @name, 'baseline', true, NULL)
ON CONFLICT (id) DO NOTHING;

-- name: EnsureBaselinePolicyVersion :exec
-- Materialise version 1 of a managed-baseline policy's document.
--
-- VERSION 1, ALWAYS, and never an update. policy_versions is APPEND-ONLY at two
-- levels (REVOKE UPDATE/DELETE plus a trigger that stops even the owner), because
-- every transaction pins the version that decided it: rewriting a version would
-- rewrite why a past, immutable record was flagged. So when the shipped baseline
-- changes, the correct move is a NEW version appended after the tenant accepts it
-- (internal/policy.BaselineVersion says so, and M7-03 owns the flow) -- not an
-- edit here. This statement therefore does nothing at all once version 1 exists,
-- whatever the binary now says.
--
-- The conflict target is the (tenant_id, policy_id, version_no) key rather than
-- the primary key, so it holds even if a caller passed a different id for the
-- same version -- the row that exists wins either way.
INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document, created_by)
VALUES (@id, @tenant_id, @policy_id, 1, @document, NULL)
ON CONFLICT ON CONSTRAINT policy_versions_no_key DO NOTHING;

-- name: EnsurePolicyAttachment :exec
-- Bind a materialised baseline policy to a resource ('*' tenant-wide for every
-- shipped baseline document today).
--
-- ⚠️ NOTHING READS THESE ROWS YET, and writing them anyway is a decision rather
-- than an oversight. policy.Evaluate scopes a statement by the Resource patterns
-- INSIDE its document, not by attachments, so the Set assembled for a tap is
-- identical with or without them. They are written because a materialised policy
-- with no attachment row is a half-provisioned policy, and the panel (M6-09) and
-- provisioning (M7-03) will both read attachments to answer "where does this
-- apply" -- a question that would otherwise get the wrong answer for every tenant
-- provisioned by this path.
INSERT INTO policy_attachments (tenant_id, policy_id, resource)
VALUES (@tenant_id, @policy_id, @resource)
ON CONFLICT ON CONSTRAINT policy_attachments_resource_key DO NOTHING;
