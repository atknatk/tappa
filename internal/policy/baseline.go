package policy

// This file is the TAPPA-MANAGED BASELINE (ADR 0004 §4; M3-06 card; CLAUDE.md §5
// rows 6-7). The baseline is the middle of the three layers: guardrails (code,
// immutable) run first and are terminal; the baseline is Tappa's managed default
// policy, attached to EVERY tenant at provisioning (M7-03); the tenant layer is
// the customer's own policies on top. Unlike a guardrail, a baseline statement
// CAN be disabled or replaced by the tenant (policies.enabled, or a more-specific
// tenant override) — but it can never loosen a guardrail, because guardrails are
// consulted first and stop on the first match (evaluate.go).
//
// WHY THIS LIVES IN CODE, NOT THE DB. The baseline is Tappa-authored and shared
// across all tenants, so its SOURCE OF TRUTH is this file (versioned with the
// binary). M3-06's job is to PACKAGE it; MATERIALISING it per tenant — writing
// policies + policy_versions + policy_attachments rows — is M7-03 provisioning's
// job. Keeping the source here also keeps internal/policy PURE (no DB, no HTTP,
// no time — same discipline as the evaluator and validator): Baseline() is a pure
// function that M7-03 persists and that the evaluator can run in-memory for tests
// and the (deferred) simulator.
//
// TWO KINDS OF BASELINE STATEMENT (both required — M3-06 card):
//
//   - TAP-EVIDENCE statements (base:ip-or-gps-ok, base:no-evidence-review, …)
//     package §5 rows 6-7 and the gaps found in review (Q15-Q17, Q21, Y7, Y-E).
//     They decide tap:record outcomes: allow or review, NEVER deny/ignore/redirect
//     (deny/ignore/redirect for taps are guardrail territory — §5 rows 1-5).
//
//   - AUTHORIZATION statements (base:authz-owner, base:authz-manager) grant panel
//     actions by role. These are NOT optional decoration: the engine fails CLOSED
//     for authorization (ADR 0004 §3 — no policy matched ⇒ deny), so a freshly
//     provisioned tenant with NO authorization policy would lock EVERYONE out of
//     the panel (K2). The baseline therefore ships role-based allows so an owner
//     (and a manager, within limits) can use the panel from day one. M7-03 MUST
//     attach these too, not only the tap statements.
//
// FIVE ACCEPTANCE INVARIANTS this file must satisfy (proven in baseline_test.go):
//  1. Every baseline document passes Validate(doc, LayerBaseline, DefaultLimits()).
//  2. No baseline statement produces ignore or redirect (only allow / review) —
//     those two effects are guardrail-only (ADR 0004 §6).
//  3. A tenant document may not claim a base: sid (reserved — validate.go); a
//     baseline document may.
//  4. With guardrails present but not matching, each baseline statement yields its
//     documented effect; the authorization allows prevent the fail-closed lockout.
//  5. Every statement can be overridden by a MORE-SPECIFIC tenant statement
//     (ADR 0004 §7) and out-restricted by an equal-specificity tenant review — and
//     none of that touches a guardrail.

import "github.com/google/uuid"

// BaselineVersion stamps every document in the managed baseline (it is written into
// each document's Version). Bumping it means a NEW baseline ships with the binary;
// from then on M7-03 provisions the new version to NEW tenants.
//
// 🔴 WHAT THE STAMP MARKS, AND WHAT IT DOES NOT — STATED BECAUSE IT NOW MARKS TWO
// DIFFERENT TEXTS AND NO MECHANICAL GATE SEES THAT. It used to say the stamp covered
// "the ENTIRE managed baseline", which read as a promise that one date identifies one
// set of documents byte for byte. It does not: SidGPSConflictReview's human-readable
// Reason was corrected in this card (T23 — the old sentence claimed a narrowing the
// condition does not test) while this constant stayed at 2026-07-26, so the same
// stamp now labels the pre- and post-correction wording. The gate that might have
// caught it, baseline_test.go's TestBaseline_VersionStamp, asserts only that every
// document carries the SAME stamp — its INTERNAL consistency — never that a given
// stamp means given CONTENT. Nothing in this repository asserts the latter.
//
// 🔴 WHY IT WAS DELIBERATELY NOT BUMPED, WHICH IS THE PART A READER WILL COME LOOKING
// FOR. A bump is an ANNOUNCEMENT, not a version number: it puts a pending-baseline
// banner on every tenant's policies screen and an INFO line on every tap, and the
// flow that lets a tenant then ACCEPT it (surface → accept → append version 2) is
// M7-03 and does not exist yet. Announcing an upgrade nobody can accept, for a change
// that alters a human-readable reason only — never an effect, condition, action or
// resource, so never a verdict — would be noise standing in for a fix. The full
// argument, the measured tenant counts and the two roads not taken are on
// SidGPSConflictReview's own statement below, at the place the text changed.
//
// ⚠️ SO PAST RECORDS ARE EXPLAINED BY THE DOCUMENT, NOT BY THIS DATE, and that is
// what keeps the situation inside §4.3/§4.6 rather than in breach of them:
// policy_versions.document is a jsonb copy of the WHOLE document as it stood, and
// every transaction pins the policy_version_id that decided it (M3-07). A row written
// under the old wording still prints the old wording. The stamp is a provisioning
// marker; the document is the record.
//
// EXISTING tenants are NOT auto-updated (M3-06 card): a silent baseline change
// would silently change a tenant's attendance report (why a past tap was flagged).
// Instead, provisioning materialises a tenant's own policy_versions rows
// (append-only, M3-02); a baseline bump is SURFACED to the tenant, who accepts it,
// at which point a new version is appended for them. Until then their materialised
// version stands, and — because policy_versions is append-only and every
// transaction pins the policy_version_id that decided it (M3-07) — past records
// keep explaining themselves under the exact version that judged them.
const BaselineVersion = "2026-07-26"

// Baseline statement sids (base: namespace, reserved from tenant documents in
// validate.go). Exported so the panel (M6-09) can render "managed by Tappa"
// controls and M7-03 can reference them by name. Their effects and conditions are
// the M3-06 card table, verbatim.
const (
	SidIPOrGPSOK         = "base:ip-or-gps-ok"        // §5 row 6, IP branch
	SidGPSOnlyAllow      = "base:gps-only-allow"      // §5 row 6, GPS-only branch (Q16)
	SidNoEvidenceReview  = "base:no-evidence-review"  // §5 row 7
	SidQRRequiresIP      = "base:qr-requires-ip"      // Q15 — QR needs IP, GPS is not enough
	SidCrossLocationNote = "base:cross-location-note" // Q17 — tap at a non-home location is normal
	SidQueuedWindow      = "base:queued-window"       // Y7 — a stale/queued occurred_at
	SidCtrGapReview      = "base:ctr-gap-review"      // Q21 — URL-hoarding's only real signal
	SidGPSConflictReview = "base:gps-conflict-review" // Y-E — an on-site proxy (IP ok, GPS far)
	SidAuthzOwner        = "base:authz-owner"         // panel authority for the owner
	SidAuthzManager      = "base:authz-manager"       // restricted panel authority for a manager
)

// BaselineQueuedSkewSeconds is base:queued-window's review threshold: a tap whose
// server-computed occurred_at skew (created_at − occurred_at) exceeds this is sent
// to review as likely queued/offline (Y7). It is tenant-TUNABLE (a baseline
// parameter, not a guardrail) and MUST stay within sys:occurred-at-bound's 0..72h
// hard-reject range (validate.go enforces this via BoundedParams). 120 s is a
// deliberate, conservative default: a live tap round-trips in seconds, so two
// minutes of skew is a strong queued/backdated signal while leaving margin for a
// slow phone or flaky link. FLAGGED for review — the orchestrator/pilot may retune
// it; it is a shipped default, not a hard rule.
const BaselineQueuedSkewSeconds = 120

// roleManager mirrors the admin_users.role CHECK ('owner','manager') from
// db/migrations/00006_create_admin_users.sql — the SAME closed vocabulary source
// guardrails.go documents for roleOwner. The panel has exactly two roles today;
// adding a third is a schema change (a new CHECK value) that would extend the
// authorization baseline here.
const roleManager = "manager"

// channelQR mirrors the channel vocabulary (nfc|qr|manual) — see guardrails.go's
// channelNFC and internal/sun.Channel / db/migrations/00005. base:qr-requires-ip
// keys on it; the value is server-derived (ADR 0004 §8), never a client flag.
const channelQR = "qr"

// tapScope is the resource pattern every tap-evidence baseline statement carries.
// location/* (a type wildcard, specificity specType) makes the statements apply at
// every location by default AND lets a tenant override at ONE branch with a
// location/<id> statement (specExact > specType, ADR 0004 §7 — the Rusty Bar
// exception). Using * (specGlobal) instead would force the tenant's only lever to
// be all-branches-at-once.
var tapScope = []string{"location/*"}

// authzScope is the resource pattern the authorization baselines carry. * because
// panel authority is role-based, not tied to one location.
var authzScope = []string{"*"}

// BaselineDoc is one canonical managed-baseline document plus the resources M7-03
// attaches it to. It is the exact unit the provisioner persists: Name -> a
// policies row (layer='baseline', created_by=NULL, the system author), Document ->
// a policy_versions row (the jsonb document), Attachments -> policy_attachments
// rows. Each behaviour is its OWN document so a tenant can enable/disable or
// replace ONE behaviour (policies.enabled is per-policy) without touching the
// rest — e.g. flip only base:gps-only-allow to require IP (Q16), or detach
// base:ctr-gap-review from a busy branch (Q21).
type BaselineDoc struct {
	// Name is the human-facing policy name (policies.name), also copied into
	// Document.Name so the persisted document is self-describing.
	Name string
	// Document is the IAM-shaped policy document (policy_versions.document). It is
	// GUARANTEED to pass Validate(doc, LayerBaseline, DefaultLimits()) (test).
	Document Document
	// Attachments are the resources this policy binds to (policy_attachments.
	// resource). "*" means tenant-wide. base:ctr-gap-review ships tenant-wide too
	// but is documented as detachable per branch (see Baseline()).
	Attachments []string
}

// Baseline returns the complete Tappa-managed baseline: the eight tap-evidence
// documents (M3-06 card table) followed by the authorization document. This is the
// SOURCE OF TRUTH and the exact input M7-03 provisioning consumes — one
// policies/policy_versions(/policy_attachments) set per BaselineDoc, per tenant.
// It is pure and deterministic (stable order, no DB, no time), so the returned
// slice is safe to persist, to feed the evaluator, and to diff across versions.
func Baseline() []BaselineDoc {
	return []BaselineDoc{
		// --- §5 row 6: IP or GPS proves place -> allow. Split into two documents
		// so a tenant can require IP by disabling/overriding ONLY the GPS-only
		// branch (Q16) while IP-proven taps keep flowing.
		tapDoc("Tappa baseline — IP proof of place", SidIPOrGPSOK, EffectAllow,
			Condition{OpBool: {CtxTapIPMatch: true}},
			"network proof of place: the source IP matches the location"),

		tapDoc("Tappa baseline — GPS-only allowance", SidGPSOnlyAllow, EffectAllow,
			// ipMatch=false AND gpsMatch=true. This is the ONE statement a tenant
			// flips to review with a single toggle to require IP everywhere (Q16);
			// IP-proven taps (base:ip-or-gps-ok) are unaffected.
			Condition{OpBool: {CtxTapIPMatch: false, CtxTapGPSMatch: true}},
			"verified via GPS only — no network proof of place"),

		// --- §5 row 7: no evidence at all -> review (never a silent allow, §4.6).
		tapDoc("Tappa baseline — no evidence needs review", SidNoEvidenceReview, EffectReview,
			Condition{OpBool: {CtxTapIPMatch: false, CtxTapGPSMatch: false}},
			"neither IP nor GPS could place this tap"),

		// --- Q15: a QR tap (photographable, timeless — no physical touch proof)
		// with no IP match goes to review; GPS alone is not enough. Composes with
		// base:gps-only-allow: a QR+GPS-only tap matches BOTH, and review out-
		// restricts allow, so it lands on review (the Q15 outcome).
		tapDoc("Tappa baseline — QR requires IP", SidQRRequiresIP, EffectReview,
			Condition{OpStringEquals: {CtxTapChannel: channelQR}, OpBool: {CtxTapIPMatch: false}},
			"QR tap without an IP match: GPS alone is not sufficient proof of place"),

		// --- Q17: tapping a location other than the employee's home location is
		// NORMAL (chain staff move between branches). Affirmed as allow + a note so
		// a cross-location tap is not penalised for that alone; the shift is
		// resolved from the TAPPED location (M4) and the crossLocation flag is
		// recorded (M3-07) for the report. A more-restrictive baseline (e.g.
		// no-evidence-review) still wins when its condition holds, so this allow
		// never opens a hole.
		tapDoc("Tappa baseline — cross-location note", SidCrossLocationNote, EffectAllow,
			Condition{OpBool: {CtxEmployeeCrossLocation: true}},
			"tapped at a location other than the employee's home location (recorded as cross-location)"),

		// --- Y7: a tap whose client-declared occurred_at is more than the tunable
		// window in the past is likely queued/offline -> review. The hard upper
		// bound (0..72h reject) is the guardrail sys:occurred-at-bound; this is the
		// softer, tenant-tunable review threshold below it.
		tapDoc("Tappa baseline — queued tap window", SidQueuedWindow, EffectReview,
			Condition{OpNumericGreaterThan: {CtxTapOccurredAtSkewSeconds: float64(BaselineQueuedSkewSeconds)}},
			"declared tap time is well in the past; likely a queued or backdated tap"),

		// --- Q21: a positive ctr gap (ctr − last_ctr − 1 > 0) means the chip was
		// tapped more times than the server saw — URL-hoarding's ONLY observable
		// signal (A1). Resource-scoped: it ships tenant-wide, but a busy branch
		// (KF St Julians) can override it at that one location (a location/<id>
		// tenant allow beats this location/* review, ADR 0004 §7) or M7-03 can
		// attach it selectively — because hoarding only pays off at LOW-traffic
		// plaques (a busy plaque's hoarded URLs die on the next person's tap).
		tapDoc("Tappa baseline — counter gap review", SidCtrGapReview, EffectReview,
			Condition{OpNumericGreaterThan: {CtxTapCtrGap: float64(0)}},
			"the tag counter jumped: reads happened that the server never saw"),

		// --- Y-E: GPS was read but is far from the location. The ATTACK this exists
		// for is an on-site proxy (a cheap phone on the venue Wi-Fi relaying a remote
		// tap), which is invisible in the GPS-only rate and caught only by this
		// dedicated conflict signal. review.
		//
		// 🔴 THE REASON USED TO SAY "IP matched but …" AND THAT WAS FALSE (backlog
		// T23, corrected 2026-08-19). The condition below tests ONE key, and
		// tap.gpsFacts computes it from DISTANCE ALONE — `conflict = have && !match`,
		// with no reference to the address at all. So this statement fires on a tap
		// with a far GPS whether or not any IP matched, and the sentence described a
		// narrowing that does not exist. It is not decoration: internal/handler's
		// policies screen prints this string to the customer verbatim, so the product
		// was explaining a mechanism it does not have. The ATTACK still involves an
		// IP; the TEST does not, and the reason must describe the test.
		//
		// ⚠️ COUNTED LIMIT, AND IT IS THE LARGER HALF: this corrects the SHIPPED
		// text, which reaches tenants provisioned from now on. Tenants whose baseline
		// was already materialised keep the old sentence, because policy_versions is
		// APPEND-ONLY and EnsureBaselinePolicyVersion does nothing once version 1
		// exists (db/queries/policies.sql says so) — measured on the development
		// database, 48 763 tenants hold a version 1 carrying the old wording.
		// Replacing it for them is a baseline UPGRADE (surface → accept → append
		// version 2), which is M7-03's flow and does not exist yet. BaselineVersion
		// was deliberately NOT bumped here: bumping is that flow's announcement, and
		// announcing an upgrade nobody can accept would put a permanent banner on
		// every policies screen and an INFO line on every tap, while changing no
		// verdict — this correction alters a human-readable reason only, never an
		// effect, condition, action or resource.
		//
		// ⚠️ 48 763 IS A DEVELOPMENT NUMBER, AND SAYING THAT CHANGES WHAT IT MEANS.
		// It was counted on the local seeded database (2026-08-19: 48 763 rows, 48 763
		// distinct tenants, all at version_no = 1). docs/plan/state.md records the
		// PRODUCTION database as empty — 0 tenants — and if that is still true then the
		// number of paying customers reading the wrong sentence today is ZERO, and the
		// whole of this limit is about a fixture. The two numbers are not
		// interchangeable and neither is quoted as the other.
		//
		// ⚠️ AND THERE IS A THIRD WAY OUT, WHICH THE FIRST WRITE-UP DID NOT NAME:
		// appending version 2 for the affected tenants directly, carrying the same
		// stamp. It was NOT taken, and §4.3 is NOT the reason — that red line binds
		// `transactions`. Three other things stop it, and only one of them is
		// mechanical: (1) policy_versions has its own append-only enforcement, verified
		// on this database — tappa_app holds INSERT and SELECT and nothing else, two
		// triggers (policy_versions_no_mutation, policy_versions_no_truncate) sit on
		// the table, and RLS is enabled AND forced; (2) the append would need a NEW
		// query, and a new query is the data layer's lens, which this phase does not
		// hold; (3) a version 2 nobody accepted changes a LIVE rule under a customer,
		// which is the thing M7-03's surface → accept → append flow exists to prevent.
		// The customer-facing sentence stays wrong for existing tenants on purpose,
		// and the reason is consent rather than immutability.
		tapDoc("Tappa baseline — GPS conflict review", SidGPSConflictReview, EffectReview,
			Condition{OpBool: {CtxTapGPSConflict: true}},
			"GPS was read and puts the device outside this location's radius"),

		// --- Authorization: role-based panel allows. ONE document (two statements)
		// because it is a single unit — the fail-closed-lockout preventer. Owner
		// gets full authority INCLUDING policy:edit: the guardrail
		// sys:policy-edit-owner-only only DENIES non-owners, so without an owner
		// allow here the owner's policy:edit would fall to the fail-closed default
		// deny and the owner could never configure anything (a lockout). Manager
		// gets the operational subset (review, approve, manual entry, reports) plus
		// employee:deactivate (customer decision, 2026-07-26). policy:edit remains
		// the ONE action a manager cannot do: it is owner-only, enforced by the
		// guardrail sys:policy-edit-owner-only (which denies non-owners) and left
		// out of the manager grant here.
		authzDoc(),
	}
}

// tapDoc builds a single-statement tap-evidence BaselineDoc scoped to every
// location and attached tenant-wide.
func tapDoc(name, sid string, eff Effect, cond Condition, reason string) BaselineDoc {
	st := Statement{
		Sid:       sid,
		Effect:    eff,
		Action:    []Action{ActionTapRecord},
		Resource:  tapScope,
		Condition: cond,
		Reason:    reason,
	}
	return BaselineDoc{
		Name:        name,
		Attachments: []string{"*"},
		Document:    Document{Version: BaselineVersion, Name: name, Statements: []Statement{st}},
	}
}

// authzDoc builds the role-based authorization BaselineDoc (owner + manager).
func authzDoc() BaselineDoc {
	name := "Tappa baseline — panel access by role"
	owner := Statement{
		Sid:    SidAuthzOwner,
		Effect: EffectAllow,
		Action: []Action{
			ActionReportExport, ActionTapApprove, ActionRecordManual,
			ActionRecordReview, ActionEmployeeDeactivate, ActionPolicyEdit,
		},
		Resource:  authzScope,
		Condition: Condition{OpStringEquals: {CtxActorRole: roleOwner}},
		Reason:    "organisation owner: full panel authority",
	}
	manager := Statement{
		Sid:    SidAuthzManager,
		Effect: EffectAllow,
		Action: []Action{
			ActionReportExport, ActionTapApprove, ActionRecordManual,
			ActionRecordReview, ActionEmployeeDeactivate,
		},
		Resource:  authzScope,
		Condition: Condition{OpStringEquals: {CtxActorRole: roleManager}},
		Reason:    "manager: review, approve, manual entry, reports and employee deactivation",
	}
	return BaselineDoc{
		Name:        name,
		Attachments: authzScope,
		Document:    Document{Version: BaselineVersion, Name: name, Statements: []Statement{owner, manager}},
	}
}

// BaselinePolicies returns the managed baseline as evaluator Policies, one per
// BaselineDoc in order, each stamped with the version id versionID(name) returns.
//
// The evaluator needs a policy_versions id on every Policy so a Decision can carry
// the exact version that decided (ADR 0004 §9, M3-07). In production M7-03 passes
// the real id it persisted for this tenant; a test or the (deferred) simulator
// passes any stable id. Order is preserved so the deterministic baseline-first,
// document-order tiebreak (evaluate.go) is honoured.
func BaselinePolicies(versionID func(name string) uuid.UUID) []Policy {
	docs := Baseline()
	out := make([]Policy, 0, len(docs))
	for _, d := range docs {
		out = append(out, Policy{
			VersionID: versionID(d.Name),
			Layer:     LayerBaseline,
			Document:  d.Document,
		})
	}
	return out
}
