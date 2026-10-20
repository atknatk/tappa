# Rules, rule change review, Billing, Freeze month

Boards: `Rules` · `T-Rules` · `M-Rules`, `PolicyChange` · `T-PolicyChange` · `M-PolicyChange`,
`Billing` · `T-Billing` · `M-Billing`, `FreezeMonth` · `T-FreezeMonth` · `M-FreezeMonth`. Citations
refer to **HEAD `b284899`** (line numbers in `.templ` files count comments). Decisions and
constraints: [README](../README.md). Status words: [pass-ledger](pass-ledger.md).

## 0. Cross-cutting

| # | Fact | Where |
|---|---|---|
| X1 | All four screens stay on `PanelShell` (no script) | `admin.templ:340-383`; `TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty` (`dashboard_test.go:563`) |
| X2 | No inline style, no `data:` URI in CSS | `adminCSP` (`adminlogin.go:1782`); inline `<svg>` is fine CSP-wise **except on `/admin/policies`** (X4) |
| X3 | Sub-page titles ("Review this change", "Freeze September 2026") and kickers use the M12-01 Title / Kicker slots | today they render as `<h2>` (`policychange.templ:64`, `billing.templ:198`) |
| X4 | **The Rules section is inside two allow-list nets.** `TestPoliciesSection_RendersOnlyInertMarkup` (`policies_test.go:537`): tags `section div p span h2 dl dt dd strong form input button select option label fieldset legend a`, attributes `class method action href type name value maxlength required` (`:946-966`). The guarantee block (`PolicyGuarantees`) only `section div p span h2`, `class` only, no touch-target class (`:908-941`). `TestPolicySectionAllowList_HasNoDeadEntry` (`:2389`) fails if an allowed tag stops being used. | On `/admin/policies`: **no `<ol>/<li>`, `<svg>`, `<h3>`, `id`/`for`, `aria-*`, `role`, `checked`, `disabled`, `<details>`.** The port does not widen either list. |
| X5 | Every state change is two POSTs: a confirmation page (writes nothing, mints a MAC bound to ONE change) → apply | policies `/admin/policies/change` → `/apply` (`policyactions.go:85-91`, `dashboard.go:204-205`); billing `/admin/billing/close` → `/apply` (`billingactions.go:74-78`, `dashboard.go:217-218`). No GET to either confirmation — a board link becomes a POST form. |
| X6 | Owner-only | Billing read + freeze = `mayManageBilling` (`billingactions.go:162-164`; tab `OwnerOnly`, `adminview.go:348`; manager → 403, `billing.go:161-168`). Rules: read for both roles; controls only when the **engine** grants `policy:edit` (`a.scribe.May`), owner-only by `sys:policy-edit-owner-only` (`internal/policy/guardrails.go:598-610`) and the stored `base:authz-owner` (`baseline.go:441-468`). |
| X7 | D2: dockets and stamps stay sharp; cards may be `rounded-[18px]` | stamps map to existing `stamp--approved` / `stamp--flagged` |

---

## 1. Rules — `/admin/policies`

### 1.1 Wiring

- Route `GET /admin/policies` (`internal/handler/dashboard.go:73-74`; href `adminview.go:321`).
- Handler `policiesSection` (`internal/handler/policies.go:98-133`) → `a.rules.Screen`
  (`internal/domain/tenant/rulebook.go:557-661`; `ListPolicySet`, `ListPolicyVersions`,
  `ListPolicyAttachments`, `ListPolicyVenueTargets`, `ListPolicyDepartmentTargets`, `GetTenantClock` in
  one `WithTenant`), `a.scribe.May`, `policyProblem` (`policies.go:135-245`), `fillPoliciesView`
  (`:247-371`).
- Templ `web/templates/pages/policies.templ`: `AdminPolicies` 50-126, `policyDegraded` 139-171,
  `policyIntro` 174-226, `policySettings` 249-279, `PolicyGuarantees` 314-348, `policyBaseline` 351-365,
  `policyTenant` 368-400, `policyRule` 407-518, `policyRuleControls` 532-568, `policyAuthoring` 585-678,
  `policyAuthorities` 686-805, `policyToneClass` 825-838.
- View model `web/templates/pages/policiesview.go`: `PoliciesView` 252-370, `PolicyGuardrailView` 48-70
  (**no control field**; field names may not contain `enable disable toggle switch on off active state
  status control href action form` — `policyGuardrailControlWords`, `policies_test.go:2436-2439`),
  `PolicyRuleView` 115-169, `PolicySwitchView` 178-188, `PolicyStatementView` 74-82, `PolicyVersionView`
  86-112, `PolicySettingView` 225-230, `PolicyAuthorityView` 234-249.

### 1.2 What the engine does (corrections the boards needed — D1)

**Guardrails: 11, in this order** (`policy.Guardrails`, `internal/policy/guardrails.go:302-623`; the
panel numbers them from the slice, `rulebook.go:778-792`): 1 `sys:tenant-mismatch` (redirect, no record;
not between peers in one holding) · 2 `sys:tag-not-active` (deny; alert when `lost`) · 3 `sys:sun-invalid`
(NFC) · 4 `sys:no-session` (redirect, no record) · 5 `sys:employee-deactivated` (deny + alert) · 6
`sys:tap-freshness` · 7 `sys:occurred-at-bound` · 8 `sys:person-debounce` (ignore) · 9
`sys:cross-tenant-qr-site-ip` (review) · 10 `sys:policy-edit-owner-only` · 11 `sys:no-self-review`. The
board's seven renumbered plates are **corrected**: print `g.Order` from the engine (`policiesview.go:49-52`).

**Baseline: 9 documents** (`policy.Baseline()`, `internal/policy/baseline.go:282-422`, stamp
`BaselineVersion = "2026-10-18"`): `base:ip-or-gps-ok`, `base:gps-only-allow`, `base:no-evidence-review`,
`base:qr-requires-ip`, `base:cross-location-note` (**note**, ADR 0029), `base:queued-window`,
`base:ctr-gap-review`, `base:gps-conflict-review`, `base:authz-owner` + `base:authz-manager` (one document,
shown under "Who may do what").

**Order:** guardrails first-match-terminal; then baseline + tenant **weighed together** — most specific
resource, then most restrictive (deny > ignore > review > allow), then a fixed walk; `note` statements are
not candidates; nothing matched → engine **default** (`MatchedSid="default"`: taps → review, else deny)
(`internal/policy/evaluate.go:19-44`). "First match wins · read top to bottom" is **REMOVED** (false below
the guardrails).

**The last rung** is the engine default, not `base:no-evidence-review` (an ordinary switchable rule).

**Editing model** (Q22, `docs/plan/m3-policy-motoru.md:16-35`): (1) switch a stored rule off/on (op
`disable`/`enable`, `policies.go:441-477`; `tenant.RuleWriter.SetEnabled`, `rulewriter.go:510`); (2) write
your own version of an authorable shipped rule (op `author`; `copyOfShipped`, `rulewriter.go:1121-1151`,
copies effect, action, condition, reason verbatim — **the effect cannot be chosen**; sid becomes `own:*`);
(3) bind / unbind (bookkeeping; the engine ignores it, `policies.templ:204-208`, `:474-482`). One
confirmation per change. The board's per-rule Approve / Spike / Reject picker is **REMOVED (D1)**.

### 1.3 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Kicker | — | DERIVE | "Guarantees first · then your rules, weighed together" |
| Title "Rules" | shell | EXISTS | |
| Header meta | `len(v.Guardrails)` (= 11); `len(v.Baseline) + len(v.Tenant)` | DERIVE | "11 guarantees · N rules of yours"; "last saved … by the owner" **REMOVED** (switches write no version) |
| Degraded notice | `v.GuardrailsOnly`, `v.Unreadable` (`policies.templ:139-171`) | EXISTS — **keep** | above everything (`policiesview.go:276-287`) |
| Ceiling / problem notices | `VersionsCapped`, `AttachmentsCapped`, `ScopeCapped`, `Problem` (`policies.templ:53-103`) | EXISTS — **keep** | incl. T125's `cross-location-first`, `evidence-off` (`policies.go:152-245`) |
| Ladder rail | — | DERIVE (CSS) | class-only `.ladder` (X4) |
| Guardrails heading + lock | `PolicyGuarantees` h2 "Taptime guarantees" (`policies.templ:316`) | EXISTS | literal "Taptime guarantees" pinned (`policies_test.go:636`); lock = CSS glyph `.lock-glyph` (no `<svg>`) |
| Rung number badge | `g.Order` (`policies.go:276-279`) | EXISTS | 1–11 from the engine; zero-pad is render-only |
| Riveted plate | — | DERIVE (CSS) | `.plate` with pseudo-element rivets |
| Plate condition text | `g.Reason`, `g.Rule` (`rulebook.go:803-826`), `g.Outcome` | DERIVE | short "when" phrases: a sid → phrase map **in the domain beside `guardrailRules`** (11 entries, both-directions coverage test like `TestGuardrailRules_CoverTheShippedSlice`); field name e.g. `When` (passes the control-word net) |
| Plate effect label | `g.Effect`; `effectSentence` (`policies.go:621-639`) | DERIVE | `effectLabel`: allow → Approve, review → Send to The Spike, deny → Reject, ignore → Ignore (recorded, counts nothing), redirect → No record; "+ security alert" from an `Alerts` flag copied into `tenant.Guardrail` (`rulebook.go:161-183`; only `lost` for tag-not-active); "Send them to their own" **REMOVED** → "No record (other business)" |
| Guardrail sid | `g.Sid` | EXISTS | |
| `g.Rule` + tunable box | `policies.templ:332-343` | EXISTS — **keep** | the plate's second line (small, ink/70); tunable box for debounce / freshness / skew. **M12-50 first:** the shipped `g.Rule` for `sys:tap-freshness` and `sys:occurred-at-bound` says the value is "yours to set" (`rulebook.go:813-816`, backlog T72) — false; the boards already show the corrected "Taptime sets the window / the tolerance" |
| Guardrails 6, 7, 10, 11 | engine | EXISTS — **keep** | 10–11 may sit in a sub-group "Also locked for the panel" |
| "Your rules" headings | `policyBaseline` h2 "Taptime baseline", `policyTenant` h2 "Your own policies" | EXISTS | "Taptime baseline" and "Who may do what" pinned (`policies_test.go:636`); two lists stay two lists |
| Rule rung number | — | **REMOVED** | baseline / tenant rows are unnumbered (no order among them) |
| Rule title | `r.Name` | DERIVE | strip "Taptime baseline — " for display only (names are also form values) |
| Rule sub-line | `Statements[0].Reason` (`policies.templ:447-449`) | EXISTS | verbatim |
| Rule sid | `Statements[i].Sid` | EXISTS | |
| CHANGED chip | — | **REMOVED (D1)** | no client state |
| Effect picker | — | **REMOVED (D1)** | replaced by a **read-only effect pill** (`Statements[i].Effect` + `effectLabel`) and the real switch |
| Switch | `r.Switch {Op, Label, Note}` (`policies.go:446-461`) | EXISTS (restyle) | every stored rule; `<form method=post action=r.Action>` + hidden `op`, `policy` + submit whose text names the outcome ("Switch this rule off" / "Switch this rule back on"; `policyBranchWitness`); no `role="switch"`, no `aria-pressed`; **absent** (not disabled) when `Switch.Label == ""` |
| State chip | `r.StateLabel/StateTone` (`policies.go:583-603`) + "Stored — but not deciding today" (`:487-489`) | EXISTS — **keep** | all six states (`TestPoliciesSection_TellsTheStatesApartOnThePage`) |
| Managed / Written by you · version · stamp | `policies.templ:413-425` | EXISTS — **keep** (compact) | |
| **T125 legacy-baseline caveat** | `r.Outdated` (`policies.go:517-523`) + `policy.BaselineChanges()` (`baseline.go:134-151`) | EXISTS — **keep** | on the rule row; `TestPolicyRuleView_AnnouncesABaselineChangeOnlyWhereItChangesTheRule` (`:2637`) |
| Statements (Applies to / Where / Only when) | `policies.templ:440-469` | EXISTS — **keep** | compact `dl` (no `<details>`) |
| Bound to + bind/unbind | `policies.templ:470-484`, `541-567` | EXISTS — **keep** | visually demoted (quiet buttons); also keeps `select/option/label` in use (dead-entry test) |
| History strip + "Read it in full" | `policies.templ:485-515`; `GET /admin/policies/version` (`dashboard.go:111`) | EXISTS — **keep** | |
| "Write your own version" | `policyAuthoring` (`policies.templ:585-678`) | EXISTS — **keep** | witness "Show me what this would do" |
| Last plate | engine default; `v.Recording` (`policies.templ:742-766`) | DERIVE | "Nothing decided → Sent to The Spike", sid text `default`; **outside** `PolicyGuarantees`; "Nothing approves silently. A record is always kept." scoped to "When nothing decides" (guardrails 1 and 4 write nothing by design) |
| Sticky change bar | — | **REMOVED (D1)** | each change already goes through its own confirmation |
| Aside "How a tap is judged" + four evidence rows | CLAUDE.md §5 "Dört kanıt" | DERIVE (static) | inside `policyIntro`'s section; "within {R} m" from `v.Settings` GPS ring (`rulebook.go:509-518`) |
| Aside GPS note | — | DERIVE | "Location is read only at the moment of a tap. Taptime never tracks anyone in the background." (§4.2) |
| Settings that are not rules | `policySettings` (`policies.templ:249-279`) | EXISTS — **keep** | in the aside |
| Who may do what | `policyAuthorities` (`policies.templ:686-805`) | EXISTS — **keep** | full width; closing notice tied to `TestPoliciesSection_SaysWhetherTheEngineIsConsulted` (`:1886`) |
| "Only the owner can save rule changes" | `policyIntro` MayEdit arms (`policies.templ:209-222`) | KEPT AS CODE | the engine's answer, not a role claim |
| M-Rules "Every tap climbs this ladder…" | — | **REMOVED (D1)** | same false first-match claim |

### 1.4 Breakpoints

- `<div class="policy-section">` (test boundary, `policies.templ:41-52`) becomes the grid: `grid
  grid-cols-1 gap-6 desk:grid-cols-[minmax(0,1fr)_17.5rem] desk:gap-7` (at 864 px: ~556 + 280); notices
  `col-span-full`.
- **DOM order is fixed:** the intro stays the element immediately before `<div class="policy-guarantees">`
  and ends in `</section>` (`policies_test.go:~545-560`): notices → `policyDegraded` → `policyIntro` (aside)
  → `PolicyGuarantees` → baseline → tenant → default plate → authoring → authorities. Place the intro in the
  right column with `desk:col-start-2 desk:row-start-1 desk:row-span-5 desk:self-start`; everything else
  `desk:col-start-1`. CSS order never changes DOM order.
- Rung gutter `pl-12 md:pl-16`; rail `left-4 md:left-[22px] w-1.5 md:w-2`; badges `w-9 h-6 md:w-[52px]
  md:h-7` (phone circles `rounded-full size-[26px]`).
- Plate grid `grid grid-cols-1 md:grid-cols-[1fr_auto] gap-x-4 gap-y-1`; rule row `flex flex-wrap
  items-center justify-between gap-3.5`.

### 1.5 Recipes

Ladder, rung, plate, lock, switch: README §5 (class-only). Aside card `bg-paper border border-line
rounded-[18px] px-6 py-5 flex flex-col gap-4`; evidence row `grid grid-cols-[2.25rem_1fr] gap-3`
(`div`/`span`, no list); number tile `size-9 rounded-[10px] bg-porcelain font-mono text-[13px] font-bold`.
Rule card `flex flex-wrap items-center justify-between gap-3.5 px-[22px] py-[18px] rounded-[14px]
bg-paper border border-line` (or keep the versioned-record `.docket`, `policies.templ:402-406`). Effect
pill `inline-flex items-center min-h-10 px-3.5 rounded-[10px] bg-paper border text-sm font-bold` + tone
(allow green, review saffron, deny tomato, note line) — not a button. Default plate `rounded-[14px]
bg-saffron-lite border border-dashed border-saffron` + `.lock-glyph`. Every new class appears literally in the
stylesheet / templ (`policyToneClass` rationale, `policies.templ:807-824`).

### 1.6 Not ported (Rules)

per-rule Approve / Send to spike / Reject picker · CHANGED chip and saffron "changed" border · sticky
"N rules changed · Review & save" bar · "First match wins · read top to bottom" · the 7-plate guardrail list
· numbered baseline rungs · `base:no-evidence-review` as the locked last rung · "Send them to their own" ·
"last saved by the owner" · header search (phone) · "changed · was …".

### 1.7 Binding copy

| Place | Code (binding) | Port |
|---|---|---|
| Intro | "How your rules are decided … weighed together … the more specific one decides … If nothing at all matches, a tap is sent to The Spike rather than quietly approved." (`policies.templ:176-186`) | keep semantics |
| Guarantees | "Taptime guarantees" + paragraph (`:316-323`); chip "Cannot be switched off" (`:330`) | heading pinned; chip stays the meaning carrier |
| Baseline | "Taptime baseline" + `:353-359` | pinned |
| Tenant empty state | `:370-392` | keep |
| Switch | "Switch this rule off" / "Switch this rule back on" + notes (`policies.go:452-461`) | never "On" / "Off" alone |
| Owner note | MayEdit arms (`:190-222`) | keep |
| Cross-location | the sentence goes to the record note, not the staff result screen (CLAUDE.md §5) | consistent |

### 1.8 Tests

`TestPoliciesSection_RendersOnlyInertMarkup` (`policies_test.go:537`) · `TestPolicyFixtures_DriveEveryConditionalBranchOfTheTemplate` (`:813`) + `policyBranchWitness` — keys are `condition@occurrence` parsed from `policies.templ`: every new `if` re-keys the map; each new branch needs a witness · `TestPolicyInertMarkupCheck_FlagsTheControlsThatBeatTheDenyList` (`:972`) · `TestPolicySection_EveryFieldNameIsOneTheServerREADS` (`:1049`) · `TestPoliciesSection_RendersNoHTMLComment` (`:1204`) · `TestPolicySectionAllowList_HasNoDeadEntry` (`:2389`) · `TestPolicyGuardrailView_CarriesNoSwitch` (`:2043`), `TestPolicyGuardrailWords_MatchTheDomainWall` (`:2449`) · `TestPolicyTones_AreClassesTheStylesheetDeclares` (`:2085`) · `TestPoliciesSection_TellsTheStatesApartOnThePage` (`:2128`) · `TestEffectSentence_TranslatesEveryEffectTheEngineHas` (`:2175`; add a twin for `effectLabel`) · `TestPolicyRuleView_AnnouncesABaselineChangeOnlyWhereItChangesTheRule` (`:2637`) · `TestPoliciesSection_SaysWhetherTheEngineIsConsulted` (`:1886`) · `TestPolicyProblemWords_EachOneRendersAHeadingAndASentence` (`:2006`) · `policyactions_test.go` `TestPoliciesSection_OffersAManagerNoControlAtAll` (`:320`) — a manager gets **no** control, not a disabled one · domain coverage test for the phrase map / alert flag.

### 1.9 Red lines

§5 — guardrails cannot be relaxed: **no control of any kind inside `PolicyGuarantees`**, not even disabled
(the M6-09 trap, `policies.templ:12-17`) · §4.6 — the default is review for taps; never present "nothing
approves silently" as a property of the whole ladder · T125 / ADR 0029 — the old-allow guard stays reachable
and explained · numbers come from the engine · rendering stays side-effect-free (`policies.templ:34-40`).

---

## 2. Rule change review — `POST /admin/policies/change` (+ `GET /admin/policies/version`)

### 2.1 Wiring

`policyChangeReview` (`policyactions.go:218-268`), `policyChangeApply` (`:270-328`), both in
`mountWriting`; `policyVersionPage` (`:519-556`, `dashboard.go:111`). Views `policyChangeView` (`:558-632`),
`policyChangeFields` (`:650-668`), `policyVersionView` (`:670-709`). Templ `pages/policychange.templ`
(`AdminPolicyChange` 60-129, `AdminPolicyVersion` 157-214; `PolicyChangeView` 20-45,
`PolicyVersionPageView` 133-154). The review re-checks `May`; a refused actor is recorded and redirected
`?problem=not-permitted` (`:224-229`) — **a manager never sees this page**. MAC subject =
`op/policy/resource/based_on/sha256(name)/venues…` (`:189-197`).

### 2.2 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Crumbs "Rules / Review a change" | `v.BackHref` | DERIVE | |
| Title | op-specific `v.Heading`: "Switch this rule off", "Switch this rule back on", "Record where this rule applies", "Stop recording this rule as applying there", "Write a new rule of your own", "Write a new version of your rule" (`policyactions.go:569-608`) | EXISTS | Title slot; the board's generic "Review this change" **REMOVED** |
| "Preview as Owner / Manager" | — | **REMOVED (D1)** | |
| Kicker | `Rule.Managed` via `a.rules.Screen` (existing read, no new SQL) | DERIVE | "Taptime baseline" / "One of yours"; "Rung 10" **REMOVED** |
| Rule title | `Rule.Name` | DERIVE | replaces the policy UUID printed today (`:617`); `author` without a policy → `c.name` |
| sid · version line | `Statements[0].Sid`, `VersionNo` | DERIVE | "vN → vN+1" only when superseding; enable/disable write **no version** → "version N · switched on → switched off"; new rule "→ v1" |
| Before / After | from Screen + `c` | DERIVE | **op-truthful only**: disable / enable (in force ↔ switched off), author new (— → your copy: effect at venues), supersede (vN where → vN+1 where), bind / unbind (bound list, "bookkeeping, not scope"); the board's effect-change diff **REMOVED (D1)**; no predicted outcome for any tap (no simulator, M9-06) |
| "In plain words" callout | `v.Warning` per op (`:571-610`) | EXISTS (restyle) | per-rule risk sentences **not ported** (no sid → sentence map) |
| dl "Applies to" / "Version in force" / "Who can save" | Statements, `Rule.History` (current), `May` | DERIVE | venue names instead of UUIDs (`venueSummary` `:634-648` → names from `ScopeTargets`; unresolvable id says so); "You can save this — the rules engine grants you policy:edit." (not "an owner") |
| Warning box "What this changes, and what nobody can tell you yet" | `components.Notice(ToneWarn, …)` + `v.Warning` + two paragraphs (`policychange.templ:75-118`) | KEPT AS CODE | keep the precise paragraph (`:105-111`: records name the rule and, if stored, the version; guarantees have no stored version; manual records name the person); restyle via `PanelNotice` |
| "Make this change" | form → `/admin/policies/apply`, hidden `Fields` + token (`:119-126`) | EXISTS | `min-h-[52px] px-6 rounded-[14px] bg-ink text-paper`; hidden fields **byte-identical** to `policyChangeFields` |
| Disabled manager button + note | — | **REMOVED (D1)** | unreachable; no disabled controls |
| Cancel | `v.BackHref` | EXISTS | |
| History panel | `Rule.History` + `HistoryNote` | DERIVE | each row an `<a>` to `/admin/policies/version?policy=…&no=…` (`policies.go:550-553`); markers from the code ("in use" / "newest stored"; "an earlier version"); when switched off nothing is "in force" (`policyactions.go:689-696`); the "2026-07-26 Taptime baseline" pseudo-row → at most "copied from the Taptime baseline of <stamp>" |
| Selected-version docket | `AdminPolicyVersion` body via `a.rules.Version` | DERIVE | current version inline (one call); older ones via links; phone inline expansion **not ported** |

### 2.3 Breakpoints and recipes

Main `flex flex-col gap-6 desk:flex-row desk:items-start`; left `min-w-0 desk:flex-[999_1_600px]`; history
`desk:flex-[1_1_380px]` (at 864 px the two wrap — acceptable, or stack under `desk`). Diff grid
`md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]`, arrow `hidden md:flex`. dl `grid sm:grid-cols-3 gap-x-5
gap-y-3 border-t border-dashed border-line pt-4`. Diff cards and pills, version row links: README §5;
callout `rounded-xl border border-tomato bg-tomato/5 px-4 py-3.5` + an inline `<svg>` triangle
(`aria-hidden`; allowed — this page is not under the policies net).

### 2.4 Tests

`policyactions_test.go`: `TestPolicyApply_CarriesTheChangeTheWarningDescribed` (`:122`),
`TestPolicyConfirmation_IsBoundToTheWHOLEChange` (`:258`), `TestPolicyConfirmation_BindsTheRuleNameTheScreenShowed`
(`:500`), `TestPolicyChange_DescribesEveryShapeOf00008` (`:608`),
`TestPolicyChange_ARefusedActorReachesNoWriteAndIsRecorded` (`:174`),
`TestPolicyVersionPage_IsBoundedAndSaysWhichVersionItIs` (`:352`), the other-business version case (`:407`),
`TestPolicyApply_TheCrossLocationGuardSaysWhatToSwitchFirst` (`:670`), `TestPolicyRoutes_TakeTheWritingChain`
(`:44`). The added `Screen()` read needs `fakeRules` support (`newFakeRules()`) and keeps "the review writes
nothing".

### 2.5 Red lines

No hidden field enters the form without entering `subject()` · no predicted outcome · never a control for a
manager.

---

## 3. Billing — `/admin/billing`

### 3.1 Wiring

Route `GET /admin/billing` (`dashboard.go:75-76`); CSV `GET /admin/billing.csv?month=` (`dashboard.go:120`).
Handler `billingSection` (`internal/handler/billing.go:161-197`): owner gate → `billingMonth` (default =
**last finished month** in the tenant zone) → `books.Period` → `books.History` (frozen months, cap 60,
`internal/domain/billing/billing.go:71`) → `fillBillingView` (`billing.go:~270-326`) with `draftStateLine`
(`:362-374`), founding (`:~420-441`), `fillBillingControl` (`:462-~480`), `fillBillingHistory`
(`:486-512`). Domain `billing.Draft` (`billing.go:202-270`); count definition `tappa_employee_is_billable`
(`db/migrations/00016_add_billing_price_and_periods.sql:219-250`: status agrees with lifecycle **and**
`activated_at IS NOT NULL` and the interval overlaps the month). Templ `pages/billing.templ` (`AdminBilling`
31-52, `billingMonthBar` 59-80, `billingDraft` 84-157, `billingHistory` 160-191). View `BillingView`
(`billingview.go:29-151`), `BillingHistoryRow` (`:158-171`).

### 3.2 Elements (layout B — resolved: the card is the **selected** month, today's behaviour)

| Element | Source | Status | Port note |
|---|---|---|---|
| "Owner only" chip | section `OwnerOnly` | DERIVE (static) | |
| Title "Billing" / kicker "What each month costs" | shell | EXISTS / DERIVE | |
| Plan line | `planLabel(d.Plan)`, `d.FirstChargeableMonth`, `FoundingHeading` | DERIVE + **correction** | the founding offer is **3** months (`tappa_first_chargeable_month`, `00016…sql:327`; "first three billing months", `billing.go:~431`); derive "free until the end of <FirstChargeableMonth − 1>" — no literal 2 or 3 in the template |
| Founding notice | `v.Founding/FoundingHeading` (`billing.templ:38-45`) | EXISTS — **keep** | "so the free period cannot end quietly" (`TestBilling_TheFoundingWarningReachesTheScreen`, `billing_test.go:400`) |
| Problem notice | `v.Problem` (`billing.templ:33-37`) | EXISTS — **keep** | |
| Month card + state | selected month (`?month=`, default last finished) | EXISTS | "running" when `!HasEnded` (only when the owner picks the current month); "October 2026 · running" as a default top card **not ported** |
| "day 20 of 31 · figure moves until the month ends" | `time.Now()` in `d.Zone`, `From`, `To` | DERIVE | only for a running month; `draftStateLine` (`:362-367`) carries the binding live-count sentence |
| Equation `N × price = amount` | `EmployeeCount`, `UnitPrice`, `Amount` (`billing.go:~311-313`) | EXISTS | real values; label "So far" (running) / "Draft" / "Frozen"; a free month = €0.00 with `FreeLine` (`:317-320`, keep); `sr-only` sentence "N people at €X each, €Y" |
| Count line | `v.CountLine` (`billing.go:314-316`) | KEPT AS CODE | keep the qualifier "provided their status agrees with their own joining and leaving dates" (the board's short version omits it) |
| Floor notice | `v.Unstamped`, `UnstampedCount` | EXISTS — **keep** | `TestBilling_SaysTheCountIsAFloorOnTheScreen` (`:133`) |
| Itemisation sentence | `v.Itemisation` | EXISTS — **keep** | "NUMBER of people, not which people" (`TestBilling_OffersNoItemisationAnywhere`, `:596`) |
| Period starts / ends (UTC) | `FromUTC/ToUTC` | EXISTS — **keep** | small mono on the receipt |
| Month picker / Prev / Next / Last finished | `billingMonthBar` (`billing.templ:59-80`) | EXISTS — **keep** | the only way to months beyond the 60-row history cap |
| Download CSV | `v.ExportHref` (`billing.templ:73`) | EXISTS — **keep** | the frozen receipts link to `billing.csv?month=YYYY-MM` as "Download CSV" |
| "Past months" grid | `v.History` (frozen only) | DERIVE | heading **"Frozen months"** (`billing.templ:162`) |
| Receipt month + range | `MonthLabel`; range from `From`/`To − 1 day` | DERIVE | `RangeLabel` on `BillingHistoryRow` |
| Receipt stamp | "FROZEN" `stamp--approved` / "DRAFT" `stamp--flagged` (`billing.go:285-299`) | KEPT AS CODE | "Ready to freeze" **REMOVED**; `TestBilling_AFrozenMonthAndADraftDoNotRenderAlike` (`:193`) |
| Receipt People / Price / TOTAL | `h.Employees` ("24 people"), `h.UnitPrice` ("€1.50 each"), `h.Amount` | EXISTS | keep the pluralised strings |
| Free / floor lines on receipts | `h.Free`, `h.Unstamped` (`billing.templ:182-187`) | EXISTS — **keep** | |
| "frozen 2 Sep" | `h.ClosedAt` (RFC3339 UTC) | DERIVE | formatted in the tenant zone |
| Freeze CTA | `CanClose`, `CloseHref`, `CloseLabel` (`billing.go:462-480`) | EXISTS — **bug fixed in M12-02** | `<form method="post" action="/admin/billing/close">` **with** `<input type="hidden" name="month" value={ v.MonthValue }>` on the selected-month card; note "Freezing fixes the figure. It can't be undone." |
| WhyNotClose | `v.WhyNotClose` (`billing.templ:152-156`) | EXISTS — **keep** | |
| "Download statement (PDF)" / "Download invoice" | — | **REMOVED (D1)** | a fifth address would fail `TestBilling_OffersNoItemisationAnywhere` |
| "View breakdown" | — | **REMOVED (D1)** | the product refuses itemisation (migration 0016) |
| Payment card / Change | — | **REMOVED (D1)** | no schema, no PSP |
| "Invoices go to …" | — | **REMOVED (D1)** | no field; ADR 0022 §10 |
| Aside "Plan" | `PlanLabel` + founding | DERIVE | plan + founding sentence take the payment card's place |

### 3.3 Breakpoints and recipes

Main `flex flex-col gap-7 desk:flex-row desk:items-start`; left `desk:flex-[999_1_600px] min-w-0`; aside
`desk:flex-[1_1_280px]`. Equation `flex flex-wrap items-end gap-x-[22px] gap-y-3.5 py-[18px] border-y
border-dashed border-line font-mono` (phone `flex-col`); N `text-[44px] leading-none font-normal`, price
`text-[28px]`, operators `text-[30px] text-ink/70` `aria-hidden`. Receipts `grid grid-cols-1
sm:grid-cols-[repeat(auto-fit,minmax(13.75rem,1fr))] gap-[22px]`; receipt = `.docket px-[22px] pt-[22px]
pb-6` (sharp) with a literal stamp class per branch. Month card `bg-paper border border-line rounded-[18px]
px-7 py-[26px] flex flex-col gap-5`. Freeze CTA `w-full min-h-11 rounded-xl bg-ink text-paper font-bold`.

### 3.4 Not ported (Billing)

running month as the default top card · "Ready to freeze" · "First 2 months free" · "Past months" heading
over a mixed grid · PDF statement / invoice · "View breakdown" · payment card · billing e-mail ·
placeholder values (`[N]`, `[PRICE]`, card numbers).

### 3.5 Tests

`billing_test.go`: `TestBilling_SaysTheCountIsAFloorOnTheScreen` (`:133`), `TestBilling_AFrozenMonthAndADraftDoNotRenderAlike` (`:193`), `TestBillingDraft_DoesNotClaimMoreVolatilityThanTheDefinitionHas` (`:259`), `TestBilling_FoundingWarningFiresWhenTheFreeWindowHasRun` (`:349`), the no-rule founding case (`:386`), `TestBilling_TheFoundingWarningReachesTheScreen` (`:400`), `TestBilling_DeclaresTheHistoryCap` (`:418`), `TestBillingMoney_PutsTheSymbolInTheRenderLayer` (`:482`), `TestBilling_OffersNoItemisationAnywhere` (`:596`; four addresses only), the month tests (`:684`, `:724`), `TestBillingRead_WritesNothing` (`:803`). `billingactions_test.go`: `TestBilling_TheSectionAndItsExportAreOwnerOnly` (`:209`), the manager tab case (`:267`). `billing_db_test.go`: `TestBillingDB_ReadingTheScreenFreezesNothing` (`:171`), the no-name case (`:470`). **New (M12-02):** render the page as owner, extract its own freeze form, post it, reach the confirmation for that month.

### 3.6 Red lines

`billing_periods` is append-only; the GET writes nothing · no itemisation, ever · no payment / e-mail UI · the
founding window never ends quietly.

---

## 4. Freeze month — `POST /admin/billing/close`

### 4.1 Wiring

`billingCloseReview` (`billingactions.go:179-240`: owner, Frozen, AfterSignup, HasEnded, mints the
confirmation); `billingCloseApply` (`:242-~270`: owner, MAC, `books.Close` **recounts** in-statement,
redirects to `billingMonthHref(m)`). View `billingCloseView` (`:435-480`); templ `AdminBillingClose`
(`billing.templ:194-239`); `BillingCloseView` (`billingview.go:179-200`).

### 4.2 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Back "‹ Billing" | `v.BackHref` | EXISTS | Crumbs |
| Kicker "Owner only · the last screen before a permanent row" | — | DERIVE | |
| Title "Freeze September 2026" | `v.Heading` (`:438`) | EXISTS | Title slot |
| Lead | `v.Summary` (`:444-447`) | EXISTS | identical wording |
| Receipt "What will be frozen" + "Check it against the Billing page" | — | DERIVE | `.docket` (sharp) |
| Stamp | — | DERIVE | `stamp--flagged` "DRAFT" (the page never shows the after-state) |
| dl groups | `v.Detail[0..8]` (`:448-454`) | EXISTS | split in the view model: `Identity`, `Figures`, `AmountLive`, `Notes` (no index-based rendering); keep the full labels (they are the definitions) |
| Founding / floor lines | conditional Detail entries (`:458-469`) | EXISTS — **keep** | |
| Irreversible warning | `components.Notice(ToneAlert)` + `v.Warning` + two paragraphs (`billing.templ:209-230`) | KEPT AS CODE | keep "so a month frozen by mistake stays as it is until a correction is designed for it" (`billingactions.go:448-450`) and the full third paragraph; restyle as the tomato-framed `role="note"` card §4.3 and the board draw (not `PanelNotice` — its alert label "Not done" would be false here; nothing has failed) |
| "Freeze this month" | form → `/admin/billing/close/apply`, hidden `month` + token (`:231-236`) | EXISTS | `min-h-14 rounded-[14px] bg-ink text-paper`; no other input |
| Cancel | `v.BackHref` | EXISTS | Freeze first, Cancel second (DOM = focus order) |
| Done state in page | — | **REMOVED (D1)** | the redirect to the frozen month **is** the done state (FROZEN stamp, `ClosedLine`) — `TestBillingClose_TheHappyPathTakesTwoStepsAndFreezesOnce` pins the Location |

### 4.3 Breakpoints and recipes

`flex flex-col gap-7 md:flex-row md:flex-wrap md:items-start`; receipt `md:flex-[1_1_380px] min-w-0`;
warning `md:flex-[1_1_420px]`. dl `grid grid-cols-[auto_1fr] gap-x-[18px] gap-y-[9px] font-mono text-[13px]`,
`dd text-right break-all`. Warning `bg-paper border-2 border-tomato rounded-[18px] px-6 py-[22px]` (`role="note"`
allowed here).

### 4.4 Not ported (Freeze)

in-page "is frozen" success state · "Ready to freeze" → "Frozen" stamp swap · shortened warning paragraphs ·
shortened phone labels.

### 4.5 Tests

`billingactions_test.go`: happy path (`:47`), `…WritesNothingWithoutTheConfirmationStep` (`:79`),
`…OnlyAnOwnerMayFreezeAMonth` (`:153`), `…RefusesAMonthThatCannotBeFrozen` (`:361`),
`…ASecondFreezeOfTheSameMonthChangesNothing` (`:406`), `…WritesNoAuditRowFromTheHandler` (`:442`),
`…ARefusedAttemptIsRecordedAtBothSteps` (`:473`), `TestBillingClose_TheWarningNamesWhatWillBeFrozenAndWhatItCannotPromise`
(`:694`), `TestBillingRoutes_AreRefusedCrossOriginBeforeTheResolverRuns` (`:643`). DB:
`TestBillingDB_FreezingAMonthWritesExactlyOneRowAndOneAuditRow` (`billing_db_test.go:252`).

### 4.6 Red lines

Freezing is permanent; figures are live and recounted at press (the page says so); no amount or price field
in the form; owner gate at both steps with audited refusals.
</content>
</invoke>
