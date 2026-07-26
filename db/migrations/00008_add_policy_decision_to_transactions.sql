-- +goose Up

-- Migration 0008 (M3-07) -- bind the POLICY DECISION to the transaction record.
-- Each tap's immutable record must carry WHY it got its verdict: which policy
-- statement (or guardrail) decided, from which append-only version, in which
-- layer, and the FULL context snapshot the decision was made from. This is an
-- ADDITIVE change to the applied migration 0005; that file is IMMUTABLE and is
-- NOT edited (CLAUDE.md section 6) -- corrections/extensions are a NEW migration.
--
-- WHY these columns live ON transactions (not a side table): the record is legal
-- evidence (section 4.3). The reason a past tap was FLAGGED must be reproducible
-- forever, even after the tenant edits the policy or the employee's shift/profile
-- changes. policy_version_id pins the EXACT append-only policy_versions row (M3-02),
-- and policy_context freezes every input that fed the decision -- so replay (the
-- M9-06 simulator) yields the same verdict and "why FLAGGED" stays correct.
--
-- KIRMIZI CIZGILER:
--   section 4.3  These four columns inherit transactions' immutability WITHOUT any
--                new statement: the table-level REVOKE UPDATE, DELETE (migration
--                0005) covers ALL columns including these (there is no column-level
--                UPDATE grant), and the BEFORE UPDATE OR DELETE trigger
--                (tappa_forbid_mutation) fires per ROW regardless of which column
--                changes. Belt+braces already wrap the new columns. Verified in
--                internal/db/rls_test.go (app UPDATE on a new column -> permission
--                denied; owner UPDATE of a new column -> append-only trigger).
--   section 4.5  RLS is row-level: transactions already ENABLE+FORCE RLS with a
--                tenant policy (migration 0005). New columns are automatically
--                subject to it -- a row invisible cross-tenant hides every column.
--                No new RLS statement is needed (re-verified in rls_test.go).
--   section 4.6  A record is NEVER lost: all four columns are NULLABLE and the
--                consistency CHECK below PERMITS the all-absent state, so a row
--                written before the M5-05 write path wires the engine in (and the
--                RLS-test fixtures) still inserts. policy_context is deliberately
--                left OUT of the CHECK for the same reason -- a tap must never be
--                dropped over a missing context snapshot (its day-one presence is an
--                M5-05 write-path guarantee, not a DB constraint).
--   section 4.7  policy_context holds the DECISION inputs, never a secret: the tap
--                engine writes tap:gpsDistanceM (a distance in metres), NOT the raw
--                GPS coordinate (CLAUDE.md section 2, section 5, section 7). No token,
--                CMAC, AES key or exact coordinate ever enters this jsonb. This is an
--                M5-05 write-path obligation; the DB cannot enforce content shape and
--                deliberately does not try (jsonb stays free-form, see below).


-- --- FK target: policy_versions (id, tenant_id) UNIQUE -----------------------
-- transactions.policy_version_id gets a COMPOSITE same-tenant FK to
-- policy_versions (id, tenant_id) so a transaction can never cite a policy version
-- of ANOTHER tenant (section 4.5, the 00005/00007 composite-FK pattern). A
-- composite FK needs a UNIQUE/PK on its exact target columns; policy_versions
-- (migration 0007) has PK(id) and UNIQUE(tenant_id, policy_id, version_no) but NOT
-- (id, tenant_id). Add it here. id is already the global PK, so (id, tenant_id) is
-- trivially unique -- this constraint exists ONLY to enable the structural
-- cross-tenant block below (same reasoning as policies_id_tenant_key /
-- transactions_id_tenant_key).
ALTER TABLE policy_versions
    ADD CONSTRAINT policy_versions_id_tenant_key UNIQUE (id, tenant_id);


-- --- transactions: the four decision columns ---------------------------------
ALTER TABLE transactions
    -- policy_version_id: the append-only policy_versions row that decided (M3-02/
    -- M3-04 Decision.PolicyVersionID). NULLABLE: a GUARDRAIL or the built-in
    -- DEFAULT decision is CODE-embedded (policy.LayerGuardrail), so it has NO stored
    -- version -> NULL. Pinning the version is what keeps a past record's reason
    -- stable after a later policy edit (section 4.3).
    ADD COLUMN policy_version_id uuid,
    -- matched_sid: the sid of the deciding statement/guardrail (Decision.MatchedSid).
    -- MACHINE-FILTERABLE on purpose (M3-07 trap): a report must be able to ask
    -- "how many taps did base:gps-only-review flag last month" with a WHERE, so the
    -- reason CANNOT live only in the free-text `note` column. Values: "sys:..." for
    -- a guardrail, a baseline/tenant statement sid ("base:...", tenant-chosen) for a
    -- stored decision, or the sentinel "default" for the no-match fallback (M3-04
    -- carries Decision.Layer=guardrail + MatchedSid="default" for the default;
    -- matched_sid is what distinguishes a real guardrail from the default). FREE-FORM
    -- text (not a CHECKed closed vocabulary): tenant sids are variable, and a closed
    -- list would force a migration per tenant rule. Presence on a decided row IS
    -- enforced by the consistency CHECK below.
    ADD COLUMN matched_sid text,
    -- policy_layer: where the deciding rule lives (Decision.Layer). CLOSED vocabulary
    -- = policy.Layer {guardrail, baseline, tenant}, an ENGINE output like verdict/
    -- channel -> a CHECK is safe and correct (an invalid layer is a bug, not client
    -- input). Unlike policies.layer (migration 0007), 'guardrail' IS allowed here:
    -- this column RECORDS what decided, and guardrails/defaults DO decide taps; it is
    -- not stored policy data. The domain + relationship is enforced by the CHECK below.
    ADD COLUMN policy_layer text,
    -- policy_context: the DECISION SNAPSHOT -- every key that fed policy.Evaluate at
    -- decision time (tap:ctrGap, tap:gpsDistanceM, tap:pageAgeSeconds, the employee's
    -- status THEN, time:minutesLate, ...). jsonb and FREE-FORM (schema validated in
    -- the app, M3-03/M5, not here) for the same forward-compat reason as
    -- policy_versions.document. Written from DAY ONE by the write path (M5-05): the
    -- past cannot be reconstructed, so it can never be back-filled. section 4.7: it
    -- holds a DISTANCE (tap:gpsDistanceM), never a raw coordinate or any secret.
    ADD COLUMN policy_context jsonb,

    -- Consistency CHECK -- mirrors EXACTLY what policy.Evaluate can emit, so it never
    -- rejects a legitimate decision (zero section 4.6 record-loss risk for correct
    -- code) and only catches a wiring BUG early -- the same bargain as the verdict
    -- CHECK in migration 0005. DECISION (M3-07 card leaves this to judgement): a
    -- scoped consistency CHECK is chosen over deferring wholly to M5, because it gives
    -- the two auditors and every report a STRUCTURAL guarantee -- "policy_version_id
    -- IS NULL <=> a guardrail/default decided" and "every decided row names a sid" --
    -- which is the whole point of M3-07 (machine-filterable, tamper-evident reasons).
    -- It is NOT over-constraining: each branch is a shape the engine actually returns.
    ADD CONSTRAINT transactions_policy_decision_consistent CHECK (
        -- (a) No policy decision recorded on this row (rows written before the M5-05
        --     write path wires the engine in; the RLS-test fixtures). All absent.
        policy_layer IS NULL
        -- (b) A GUARDRAIL or the built-in DEFAULT decided: both are code-embedded
        --     (Decision.Layer=guardrail), so NO stored version -> policy_version_id
        --     MUST be NULL; matched_sid names the rule ("sys:..." or "default").
        OR (policy_layer = 'guardrail'
            AND policy_version_id IS NULL
            AND matched_sid IS NOT NULL)
        -- (c) A stored BASELINE/TENANT policy decided: it came from an append-only
        --     policy_versions row -> policy_version_id MUST be present (the FK pins
        --     the exact version); matched_sid names the deciding statement.
        OR (policy_layer IN ('baseline', 'tenant')
            AND policy_version_id IS NOT NULL
            AND matched_sid IS NOT NULL)
    ),

    -- Composite same-tenant FK (section 4.5). MATCH SIMPLE (default): with
    -- policy_version_id NULL the FK is NOT checked, so guardrail/default rows (NULL
    -- version) are fine while a present version MUST resolve to a same-tenant
    -- policy_versions row. ON DELETE RESTRICT: policy_versions is append-only and
    -- never deleted, and a cited version must stay resolvable (evidence chain).
    ADD CONSTRAINT transactions_policy_version_fk
        FOREIGN KEY (policy_version_id, tenant_id)
        REFERENCES policy_versions (id, tenant_id) ON DELETE RESTRICT;

-- No index on matched_sid/policy_version_id here (DECISION, marked): with two
-- tenants and ~100 users the report volume is tiny, and the existing
-- (tenant_id, occurred_at DESC) index already scopes a "reasons last month" scan.
-- A dedicated index is premature; M6-11 (reports) adds one against a real query
-- pattern if measurement warrants it.


-- +goose Down

-- Reverse order: drop the transactions constraints + columns first (the FK and
-- CHECK reference the columns, so they are dropped explicitly before the columns),
-- then the policy_versions UNIQUE that only existed to be their FK target. Nothing
-- from migrations 0005/0007 (the shared trigger fn, RLS, GRANTs) is touched.
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_policy_version_fk,
    DROP CONSTRAINT IF EXISTS transactions_policy_decision_consistent,
    DROP COLUMN IF EXISTS policy_context,
    DROP COLUMN IF EXISTS policy_layer,
    DROP COLUMN IF EXISTS matched_sid,
    DROP COLUMN IF EXISTS policy_version_id;

ALTER TABLE policy_versions
    DROP CONSTRAINT IF EXISTS policy_versions_id_tenant_key;
