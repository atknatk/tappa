package checkin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
)

// Assembling one tenant's decision Set — guardrails from code, the managed
// baseline from the database, and the tenant's own policies on top.
//
// 🔴 WHY THIS FILE EXISTS AT ALL: WITHOUT IT, AN ORDINARY CHECK-IN CANNOT BE
// RECORDED. Measured against the dev database before a line of it was written,
// as tappa_app, for the seeded Kebab Factory tenant:
//
//	policies = 0, policy_versions = 0
//	INSERT INTO transactions (..., policy_layer, policy_version_id, matched_sid)
//	VALUES (..., 'baseline', gen_random_uuid(), 'base:ip-or-gps-ok')
//	  -> ERROR 23503 transactions_policy_version_fk
//	INSERT ... policy_version_id = '00000000-0000-0000-0000-000000000000'
//	  -> ERROR 23503 (the uuid.Nil wiring bug state.md tracked as N4)
//	INSERT ... policy_layer='guardrail', policy_version_id NULL
//	  -> INSERT 0 1
//
// Read that against CLAUDE.md §5: rows 1-5 are guardrail decisions and they
// insert fine, but rows 6 and 7 — the ORDINARY `ok` and the ORDINARY `flag`, the
// path almost every tap in the product's life takes — are BASELINE decisions, and
// migration 0008's consistency CHECK plus composite FK require a baseline
// decision to name a REAL, same-tenant policy_versions row. The task that was
// meant to write those rows is M7-03 (provisioning), and M7-03 has not been done.
// So the main path had no way to be persisted, which is a §4.6 record loss on
// the most common outcome there is.
//
// TWO WAYS OUT WERE CONSIDERED, AND THE CHOICE IS STATED BECAUSE IT IS A TRADE:
//
//  1. MATERIALISE ON FIRST NEED (chosen). The first tap for a tenant that has no
//     baseline writes it — idempotently, append-only, under that tenant's own
//     context. Every later tap finds it and writes nothing. The record is written
//     with a real version id and the decision is the one the product intends.
//  2. REFUSE LOUDLY and leave provisioning to M7-03. Rejected: "refuse" here can
//     only mean either dropping the tap (a §4.6 violation on the main path) or
//     evaluating with no baseline, which writes the record but sends EVERY tap in
//     that tenant to the fail-to-review default — a permanent, silent downgrade
//     of the product to "a manager approves everything by hand". Neither is
//     better than writing ten rows once.
//
// WHAT IS LEFT TO M7-03, precisely: provisioning at SIGN-UP (so a tenant's policy
// layer exists before anybody taps rather than because somebody did), the panel's
// view of it, and the BASELINE UPGRADE flow — a new BaselineVersion is surfaced
// to the tenant, who accepts it, and a version 2 is appended (internal/policy's
// BaselineVersion doc). This file deliberately does NOT upgrade: it writes
// version 1 and never touches an existing one, so a tenant's decisions cannot
// change under them because a binary was deployed. When the shipped baseline and
// a tenant's stored one disagree, it says so in a log line and evaluates on the
// STORED one — the version the record will pin.
//
// FAIL-SAFE, ALL-OR-NOTHING. If the set cannot be assembled — the load fails,
// materialising fails, a stored document will not parse — this returns the
// guardrails ALONE, never a partial baseline. A partial baseline is not a
// smaller safety margin, it is a DIFFERENT policy: drop base:qr-requires-ip while
// keeping base:gps-only-allow and a QR tap with only a GPS fix becomes `ok`
// instead of `flag`. Guardrails alone send every tap to the fail-to-review
// default, so the record is still written (§4.6) and a human sees it.

// policySets assembles per-tenant decision Sets. It holds no state between calls
// beyond its dependencies: the Set is rebuilt per tap, because a tenant may
// enable or disable a policy between two taps and a cached Set would keep
// deciding on the old one. Caching belongs with the panel that does the editing
// (M6-09), which knows when to invalidate.
type policySets struct {
	data Database
	// params are the tenant-resolved bounded windows (ADR 0004 §11) the guardrail
	// closures compare against. They come from config and are range-checked before
	// this type is built (see New).
	params policy.Params
	log    *slog.Logger
}

// baselinePolicyURN and baselineVersionURN are the uuid v5 namespaces the managed
// baseline's policy and version ids are derived in. (This doc opened with the name
// `baselineNamespace`, which no declaration in this file carries.)
//
// DETERMINISTIC IDS ARE THE IDEMPOTENCY ANCHOR *FOR policies*, and the precision
// matters because an earlier version of this comment over-generalised it. Only
// `policies` needed derived ids: at the time it had no UNIQUE (tenant_id, name), so
// an ON CONFLICT had to target either a new constraint (a migration) or one that
// already exists — and deriving the id from (tenant, document name) makes its
// PRIMARY KEY that target, so two concurrent first taps produce one row rather
// than two policies with the same name. The other two statements conflict on
// constraints that were already there and are NOT primary keys:
// policy_versions_no_key (tenant_id, policy_id, version_no) and
// policy_attachments_resource_key (tenant_id, policy_id, resource). No migration
// either way; three different anchors.
//
// ⚠️ MIGRATION 00015 HAS SINCE ADDED UNIQUE (tenant_id, layer, name), so the premise
// of the paragraph above is no longer the state of the schema. The derivation is kept
// and is still the right anchor -- it makes the PRIMARY KEY a usable conflict target
// and keeps two concurrent first taps to one row -- but a reader must not take "it has
// no unique name key" as a present-tense fact. The measured consequence of the new
// index for THIS statement is none: baseline ids are derived from (tenant, name) and
// the layer is fixed, so the two indexes have the same collision set (00015's header).
//
// It is uuid.NameSpaceURL over a URL-shaped string rather than an invented
// namespace constant, so the derivation is legible in the one place it matters:
// a person holding a policy id can recompute it from the tenant and the name.
// Nothing about these ids is secret — they are opaque row keys behind RLS, and
// guessing one grants nothing that being inside the tenant does not already.
const (
	baselinePolicyURN  = "https://tappa.app/policy/baseline/"
	baselineVersionURN = "https://tappa.app/policy-version/baseline/"
)

// baselinePolicyID and baselineVersionID derive the two ids for one managed
// baseline document in one tenant. versionNo is part of the version id so that a
// future version 2 (M7-03's upgrade flow) derives a different id rather than
// colliding with version 1.
func baselinePolicyID(tenantID uuid.UUID, name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(baselinePolicyURN+tenantID.String()+"/"+name))
}

func baselineVersionID(tenantID uuid.UUID, name string, versionNo int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL,
		[]byte(fmt.Sprintf("%s%s/%s/%d", baselineVersionURN, tenantID, name, versionNo)))
}

// forTenant returns the decision Set for one tenant, materialising the managed
// baseline if it is not there yet.
//
// It returns NO ERROR by design. Every failure below is answered with the
// guardrails alone plus a loud log line, because the caller's next move is to
// RECORD A TAP and §4.6 does not allow that to be skipped over a policy-loading
// problem. The cost of the fallback is visible rather than silent: the tap lands
// on the fail-to-review default, so it is written, flagged, and put in front of a
// manager — which is what "we could not work out the rules" should look like.
func (p policySets) forTenant(ctx context.Context, tenantID uuid.UUID) policy.Set {
	set := p.guardrailsOnly()

	rows, err := p.load(ctx, tenantID)
	if err != nil {
		p.log.Error("checkin: loading the tenant policy set failed; deciding on guardrails alone",
			"tenant_id", tenantID, "err", err)
		return set
	}

	baseline, missing := assembleBaseline(rows)
	if len(missing) > 0 {
		if err := p.materialise(ctx, tenantID, missing); err != nil {
			p.log.Error("checkin: materialising the managed baseline failed; deciding on guardrails alone",
				"tenant_id", tenantID, "missing", len(missing), "err", err)
			return set
		}
		p.log.Info("checkin: materialised the managed baseline for a tenant that had none",
			"tenant_id", tenantID, "documents", len(missing), "baseline_version", policy.BaselineVersion,
			"note", "provisioning at sign-up is M7-03; this is the first-need fallback")
		if rows, err = p.load(ctx, tenantID); err != nil {
			p.log.Error("checkin: re-loading the tenant policy set failed; deciding on guardrails alone",
				"tenant_id", tenantID, "err", err)
			return set
		}
		if baseline, missing = assembleBaseline(rows); len(missing) > 0 {
			// Written, re-read, still not all there. Something is wrong that this
			// path cannot fix, and a PARTIAL baseline is a different policy rather
			// than a weaker one (see the file header), so it is not used.
			p.log.Error("checkin: the managed baseline is incomplete after materialising; deciding on guardrails alone",
				"tenant_id", tenantID, "missing", len(missing))
			return set
		}
	}

	for _, b := range baseline {
		if b.Document.Version != policy.BaselineVersion {
			// The binary ships a newer baseline than this tenant has accepted. That
			// is the DESIGNED state (internal/policy: existing tenants are never
			// auto-updated, because a silent change would rewrite why past taps were
			// flagged), so it is not an error — but it is worth seeing, because the
			// flow that resolves it (surface, accept, append version 2) is M7-03 and
			// does not exist yet.
			p.log.Info("checkin: tenant baseline is older than the shipped one; M7-03 owns the upgrade",
				"tenant_id", tenantID, "stored", b.Document.Version, "shipped", policy.BaselineVersion)
			break
		}
	}

	set.Baseline = baseline
	set.Tenant = tenantPolicies(rows, p.log)
	return set
}

// guardrailsOnly is the floor every Set is built on: the code-embedded guardrails
// and nothing else. It is also the ANSWER to every failure below (see forTenant's
// doc), which is why it is one function rather than a literal in three places.
func (p policySets) guardrailsOnly() policy.Set {
	return policy.Set{
		Guardrails: policy.Guardrails(p.params),
		// Eval-time anomalies (a stored document naming an operator this binary no
		// longer knows — only reachable after a code rollback) are reported through
		// the tap's own logger instead of the package default, so they arrive with
		// the rest of this request's logs. The evaluator's own contract is that an
		// anomaly makes the statement inert; it never changes the Decision.
		OnAnomaly: func(a policy.Anomaly) {
			p.log.Warn("policy: eval-time anomaly; statement treated as non-matching",
				"kind", string(a.Kind), "layer", string(a.Layer), "sid", a.Sid,
				"operator", string(a.Operator), "contextKey", string(a.Key))
		},
	}
}

// StoredSet is forTenant WITHOUT the materialisation — the tenant's rulebook
// exactly as it is stored today, and nothing written whatever it finds.
//
// 🔴 IT EXISTS SO AN AUTHORISATION DECISION CANNOT PROVISION A BUSINESS. M6-09
// phase B asks the engine "may this admin edit the rules" before it writes
// anything, and the obvious way to get a Set — forTenant — MATERIALISES the
// managed baseline when it finds it missing. That is right for a tap (a record
// that cannot name a policy_versions row cannot be written at all, §4.6) and
// wrong for a gate: it would mean an admin who is about to be REFUSED still
// leaves nine policies, nine versions and nine attachments behind in an
// append-only table, and it would make the panel a second provisioning path
// nobody asked for. The M6-09 card counted "the panel must not materialise" as
// the reason the gate was deferred three times; this method is that objection
// answered rather than argued with.
//
// SAME FAIL-SAFE DIRECTION, DIFFERENT COST. Every failure returns the guardrails
// ALONE, exactly as forTenant does. For a tap that means the record is written and
// flagged; for an authorisation it means the built-in fail-closed default decides,
// i.e. the act is REFUSED. Both are the safe direction for their caller.
//
// ⚠️ WHAT THAT COSTS, COUNTED RATHER THAN HIDDEN: a business whose baseline has
// never been materialised — no tap has ever happened — has no stored grant for its
// OWNER either, so the owner is refused until the first tap provisions the layer.
// That is visible on the panel (every rule reads "Not set up yet") and it is the
// same state the tap path is in; the real repair is provisioning at sign-up, which
// is M7-03's job and is deliberately NOT done from a panel request.
func (s *Service) StoredSet(ctx context.Context, tenantID uuid.UUID) policy.Set {
	return s.policies.stored(ctx, tenantID)
}

func (p policySets) stored(ctx context.Context, tenantID uuid.UUID) policy.Set {
	set := p.guardrailsOnly()
	rows, err := p.load(ctx, tenantID)
	if err != nil {
		p.log.Error("checkin: loading the stored policy set failed; answering on guardrails alone",
			"tenant_id", tenantID, "err", err)
		return set
	}
	baseline, missing := assembleBaseline(rows)
	if len(missing) > 0 {
		// ALL OR NOTHING, and nothing is written to close the gap. A partial baseline
		// is a DIFFERENT policy rather than a weaker one (the file header), and that
		// argument does not stop being true because the caller is a panel.
		p.log.Warn("checkin: the stored baseline is incomplete; answering on guardrails alone",
			"tenant_id", tenantID, "missing", len(missing))
		return set
	}
	set.Baseline = baseline
	set.Tenant = tenantPolicies(rows, p.log)
	return set
}

// load reads the tenant's stored policy layer.
func (p policySets) load(ctx context.Context, tenantID uuid.UUID) ([]store.ListPolicySetRow, error) {
	var rows []store.ListPolicySetRow
	err := p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListPolicySet(ctx, tenantID)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("checkin: list policy set: %w", err)
	}
	return rows, nil
}

// assembleBaseline pairs each SHIPPED baseline document with the tenant's stored
// row for it, in the canonical order internal/policy declares them, and reports
// which shipped documents have no row at all.
//
// THE ORDER IS THE CANONICAL ONE, NOT THE DATABASE'S. ListPolicySet comes back
// ordered by a random uuid, and the evaluator breaks a full tie by first
// encountered in a fixed walk — so using the query's order would make the sid a
// record reports depend on how uuids happened to sort. Rebuilding the slice from
// policy.Baseline() restores the determinism the evaluator promises.
//
// THE STORED DOCUMENT IS USED, NOT THE SHIPPED ONE, even though both are to hand.
// The record pins a policy_version_id, and the whole point of pinning is that the
// id and the rules it names cannot drift apart: evaluating the binary's document
// while pinning the database's version id would produce records whose stated
// reason is not the reason. A tenant that has not accepted a newer baseline is
// judged by the one they have.
//
// A DISABLED policy is neither included nor reported missing: enabled=false is
// the tenant's own off switch (00007, ADR 0004), so it is a deliberate absence,
// and reporting it missing would re-materialise it on every tap forever.
func assembleBaseline(rows []store.ListPolicySetRow) (out []policy.Policy, missing []policy.BaselineDoc) {
	stored := make(map[string]store.ListPolicySetRow, len(rows))
	for _, r := range rows {
		if policy.Layer(r.Layer) == policy.LayerBaseline {
			stored[r.Name] = r
		}
	}
	for _, doc := range policy.Baseline() {
		row, ok := stored[doc.Name]
		if !ok {
			missing = append(missing, doc)
			continue
		}
		if !row.Enabled {
			continue
		}
		parsed, err := policy.ParseAndValidate(row.Document, policy.LayerBaseline, policy.DefaultLimits())
		if err != nil {
			// A stored baseline document that will not parse cannot be evaluated and
			// must not be quietly skipped: skipping one statement changes the policy
			// (see the file header). Reported as missing so the caller falls back to
			// guardrails alone rather than to nine tenths of a baseline.
			missing = append(missing, doc)
			continue
		}
		out = append(out, policy.Policy{
			VersionID: row.VersionID,
			Layer:     policy.LayerBaseline,
			Document:  parsed,
		})
	}
	return out, missing
}

// tenantPolicies turns the tenant's OWN stored policies into evaluator policies.
//
// ✅ THE PANEL PRODUCES THESE NOW, AND THIS PARAGRAPH USED TO SAY NOTHING COULD.
// It read "there is no query in db/queries that writes a policy outside this
// package's baseline materialisation, and no endpoint that would call one ... this
// returns an empty slice in every deployment as it stands" — all three clauses were
// true when written and all three were false the moment M6-09 phase B shipped:
// CreateTenantPolicy and AppendPolicyVersion are in db/queries/policies.sql,
// internal/domain/tenant.RuleWriter.Author calls them with layer='tenant', and
// POST /admin/policies/apply reaches it. A reader of the DECISION path was being told
// the customer layer is empty in every deployment, which is the last thing to be
// wrong about here.
//
// It was written before anything could reach it for the reason that still holds: a
// decision engine that silently ignores the customer layer the moment that layer
// becomes writable is the failure ADR 0004 calls the most dangerous one ("my rule
// works" when it does not). That day has arrived.
//
// A document that does not parse or validate is DROPPED, with an error log,
// rather than failing the tap. The asymmetry with the baseline is deliberate: a
// tenant statement is an ADDITION to a working policy, and losing one leaves the
// baseline's behaviour intact, whereas a missing baseline statement changes what
// the shipped rules do. Dropping is also the more restrictive direction for the
// case that matters, since a tenant statement that would have ALLOWED something
// simply stops allowing it.
//
// The order is (name, id) rather than the query's uuid order, for the same
// determinism reason as the baseline.
func tenantPolicies(rows []store.ListPolicySetRow, log *slog.Logger) []policy.Policy {
	var picked []store.ListPolicySetRow
	for _, r := range rows {
		if policy.Layer(r.Layer) == policy.LayerTenant && r.Enabled {
			picked = append(picked, r)
		}
	}
	sort.Slice(picked, func(i, j int) bool {
		if picked[i].Name != picked[j].Name {
			return picked[i].Name < picked[j].Name
		}
		return picked[i].PolicyID.String() < picked[j].PolicyID.String()
	})
	out := make([]policy.Policy, 0, len(picked))
	for _, r := range picked {
		doc, err := policy.ParseAndValidate(r.Document, policy.LayerTenant, policy.DefaultLimits())
		if err != nil {
			log.Error("checkin: a stored tenant policy will not parse and is not being applied",
				"policy_id", r.PolicyID, "version_id", r.VersionID, "err", err)
			continue
		}
		out = append(out, policy.Policy{VersionID: r.VersionID, Layer: policy.LayerTenant, Document: doc})
	}
	return out
}

// materialise writes the missing managed-baseline documents for one tenant.
//
// ONE TRANSACTION, so a tenant never ends up with half a baseline committed — the
// state assembleBaseline would then have to treat as complete-but-different.
//
// EVERY STATEMENT IS AN ON CONFLICT DO NOTHING INSERT (db/queries/policies.sql),
// which is what makes this safe to run from a request handler at all: two taps
// arriving together write one set of rows, a tenth tap writes nothing, and an
// existing row — including one the tenant has switched OFF — is never modified.
// policy_versions is append-only by privilege AND by trigger (00007), so "write
// it again" HAS to be a no-op rather than an update; it is.
func (p policySets) materialise(ctx context.Context, tenantID uuid.UUID, docs []policy.BaselineDoc) error {
	err := p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		for _, d := range docs {
			raw, err := json.Marshal(d.Document)
			if err != nil {
				return fmt.Errorf("marshal baseline document %q: %w", d.Name, err)
			}
			policyID := baselinePolicyID(tenantID, d.Name)
			if err := q.EnsureBaselinePolicy(ctx, store.EnsureBaselinePolicyParams{
				ID: policyID, TenantID: tenantID, Name: d.Name,
			}); err != nil {
				return fmt.Errorf("ensure baseline policy %q: %w", d.Name, err)
			}
			if err := q.EnsureBaselinePolicyVersion(ctx, store.EnsureBaselinePolicyVersionParams{
				ID:       baselineVersionID(tenantID, d.Name, 1),
				TenantID: tenantID,
				PolicyID: policyID,
				Document: raw,
			}); err != nil {
				return fmt.Errorf("ensure baseline policy version %q: %w", d.Name, err)
			}
			for _, resource := range d.Attachments {
				if err := q.EnsurePolicyAttachment(ctx, store.EnsurePolicyAttachmentParams{
					TenantID: tenantID, PolicyID: policyID, Resource: resource,
				}); err != nil {
					return fmt.Errorf("ensure baseline attachment %q -> %q: %w", d.Name, resource, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("checkin: materialise baseline: %w", err)
	}
	return nil
}
