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
-- ON CONFLICT (id) DO NOTHING is what makes a second tap free. It also means an
-- EXISTING row is left exactly as it is -- including enabled=false. A tenant that
-- switched a baseline statement off must not have it switched back on by someone
-- walking up to a plaque.
--
-- 🔴 THIS COMMENT USED TO SAY "and a hundred concurrent first taps produce one
-- row". IT IS FALSE, AND THIS INSERT IS THE ONE THAT FAILS. Measured (M5-08
-- audit, barrier-synchronised COMMITTING racers, a virgin key per attempt, five
-- attempts per row):
--
--   policies            ON CONFLICT (id)                20 racers: 4 / 100 fail
--                                                       40 racers: 9 / 200 fail
--   policies            ON CONFLICT (id, tenant_id)      8 racers: 10 / 40 fail
--   policy_versions     ON CONSTRAINT ..._no_key        20 racers: 19 / 100 fail
--   policy_versions     ON CONFLICT (id)  [the PK]      20 racers:  9 / 100 fail
--   policy_attachments  ON CONSTRAINT ..._resource_key  20 and 40 racers: 0 fail
--
-- and through the REAL code path (policySets.forTenant against a virgin tenant),
-- 40 racers produce 3-4 failures per 200, every one logged as:
--
--   materialise baseline: ensure baseline policy "...": duplicate key value
--   violates unique constraint "policies_id_tenant_key" (SQLSTATE 23505)
--
-- THE RULE IS "A NON-ARBITER UNIQUE INDEX", NOT "THE PRIMARY KEY". ON CONFLICT
-- arbitrates on ONE index; the speculative insertion can still trip any OTHER
-- unique index on the row. policies carries TWO: policies_pkey (id) and
-- policies_id_tenant_key (id, tenant_id) -- the latter added by 00007 as a
-- composite FK target. Arbitrating on either one leaves the other exposed, which
-- is why both columns of the table above fail and why swapping the target only
-- moves the collision. policy_attachments never fails for a structural reason
-- instead: it does not supply id at all (see its INSERT), so gen_random_uuid()
-- gives every racer a distinct primary key and there is no second index to trip.
--
-- ⚠️ NO WORKING ARBITER CHOICE IS KNOWN. An earlier version of this comment
-- recommended "arbitrate on the PRIMARY KEY"; that is RETRACTED -- measured, PK
-- arbitration on policy_versions still fails 9 / 100, on policy_versions_no_key.
-- A real fix has to come from somewhere else (retry on 23505, an upsert that
-- names both indexes, or provisioning the baseline once at sign-up so the tap
-- path never races -- M7-03's job).
--
-- 🔬 METHOD, because this class is easy to measure wrongly and an earlier
-- measurement in this very comment reported "0 failures" for the top row:
--   * the race only exists once a COMPETING TRANSACTION COMMITS -- a harness
--     whose racers roll back cannot see it at all;
--   * it is a LOW-PROBABILITY event (~4% of inserts here), so ONE green run
--     proves nothing: run many attempts, each with a VIRGIN key;
--   * the probe must supply ids the way PRODUCTION does. The earlier probe handed
--     every racer the same policy_attachments id, which production never does,
--     and so "measured" a failure that cannot occur.
--
-- IT IS FAIL-SAFE, NOT SILENT: a losing racer's tap falls back to a
-- guardrail-only decision, so the record is still written (section 4.6) and is
-- flagged rather than counted. NOT FIXED HERE -- policyset.go is outside the
-- M5-08 diff and the fix belongs with M7-03's provisioning; the existing race
-- test uses 8 racers, below the rate at which this shows up.
--
-- ON CONFLICT (id) DO NOTHING still makes a second tap free, and it leaves an
-- EXISTING row exactly as it is -- including enabled=false. A tenant that
-- switched a baseline statement off must not have it switched back on by someone
-- walking up to a plaque.
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
