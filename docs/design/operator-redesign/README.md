# Operator surface redesign — handoff for the port

This folder is the handoff package for porting the **platform operator surface** (`/operator`,
on its own host `TAPPA_OPERATOR_HOST`) from the approved boards (2026-10-20) to templ +
Tailwind. A developer agent should be able to port every operator screen from this README,
[`screens.md`](screens.md) and the M13 task cards without discovering surprises in the code.

- **Boards:** `docs/design/panel-redesign/boards/` — the operator boards sit beside the panel's,
  one file per screen and width: `Op<Name>.dc.html` (desktop, 1440, fluid), `T-Op<Name>.dc.html`
  (tablet, 834), `M-Op<Name>.dc.html` (phone, 390). Twelve screens × three widths. How to read a
  board (bindings, sample data, inline styles that must never be copied): panel README §1.
- **Screen file:** [`screens.md`](screens.md) — per screen, the element list, binding copy,
  forms with every hidden field, states, the never-shown list and the tests to keep, with
  `file:line` citations.
- **Task cards:** [`docs/plan/m13-operator-redesign.md`](../../plan/m13-operator-redesign.md)
  (M13-01 … M13-15, ordered, each independently shippable).
- **Shared with the panel port** (by reference, not repeated here):
  [`docs/design/panel-redesign/README.md`](../panel-redesign/README.md) — D1–D4, §3 constraints,
  §4 token map, §5 component catalogue.

All `file:line` citations here and in `screens.md` refer to **HEAD `022ba3c`**. The operator
handler and template files (`internal/handler/operator/*.go`, `web/templates/operatorpages/*`)
are unchanged since `1e5cc83`, the commit the inventory was taken at, so those lines hold for
both. `web/templates/layout/base.templ` moved: `layout.Operator` is at `base.templ:558`,
`OperatorWithScript` at `:570`. `web/static/css/input.css` moved with the panel's component
work after `022ba3c`; its citations in §6 and in the M13 cards were **re-read at `a8a06f6` (unchanged at
`27516b7`)** — board polish sync, 2026-10-20; the operator handler, template and test files are
unchanged at both. Lines move; re-find them by the symbol named beside each one.

---

## 1. What the operator surface is (read before drawing a pixel)

- **A separate host and a separate shell.** Every route answers only on the operator host; any
  other host gets the router's plain-text 404 (`hostGate`, `internal/handler/operator/routes.go:136-144`).
  The shell is `layout.Operator` / `layout.OperatorWithScript` (`base.templ:558-580`): porcelain
  body, ink text, **`Theme{}`** — no tenant brand, no theme link, no logo.
- **It crosses tenants.** Every data read is an `op_*` SECURITY DEFINER call (CLAUDE.md §4.5
  exception (1), ADR 0021): `op_begin_read` writes and commits an `operator_audit_log` `read` row
  and returns a ticket, `op_read_*` consumes it. The ticket never reaches the handler package and
  has **no screen** (`audit.go:26-27`, `tenants.go:27`, `plaques.go:24-26`, `billing.go:31-34`,
  `legal.go:42-43`). The only visible consequence: *opening* a data screen is itself an audited
  read and costs one unit of the session's read budget (60 / 10 min, `surface.go:485-486`) and
  one of the request budget (100 / 10 min, `surface.go:376-377`).
- **Writes that exist** (nothing else is writable): legal publish; holding create / add member /
  remove member; integration API open / change code / close; VAT re-check (records the VIES
  verdict only). No impersonation, no tenant edit (ADR 0020 §8).
- **One script on the whole surface:** `/static/js/operator/enroll.js`, on the enrollment page.

---

## 2. The user's decisions (2026-10-20) — binding

### Inherited from the panel redesign

- **D1 — Code is the truth.** The port moves the look; it adds no product feature. Every element,
  string, form and state comes from the code (`screens.md`). Binding strings stay verbatim.
  Board sample data (names, ids, times) is never product copy.
- **D2 — Corners.** The docket and the stamp stay **sharp** (`rounded-none`, at most
  `rounded-sm`); cards, fields, buttons may round; chips are `rounded-full`.
- Palette tokens only (+ token/opacity). Space Grotesk + IBM Plex Mono at **400/700 only**.
- No new JavaScript; CSP `style-src 'self'` → no inline style in the real port (boards use
  inline styles; they are mock-ups).
- **No tenant branding anywhere** — no logo, no accent. The tenant **name** banner is mandatory
  on every one-tenant screen (ADR 0020 §9).
- **Home makes no read** → no figures, counts or widgets on Home, ever.
- **Never shown:** the operator's e-mail address, the TOTP secret or code, the enrollment token
  (except the enrollment page's designed first-load egress, §4.8), read tickets.

### OP-1 — Navigation is added (*"Menü ekle"*)

This **reverses** the "no tab bar" choice written at `web/templates/operatorpages/chrome.templ:19-21`
("No tab bar, no green wordmark, a narrower column"). The nav is **links only** — no new read,
no new route, no form. It must stay unmistakable from the tenant panel.

**The operator shell (identical on every signed-in screen):**

- **Top — the band, unchanged in markup.** `<header class="op-bar">` full width, ink, 4 px
  saffron bottom rule. Left: the lockup `TAPTIME <span class="text-saffron">OPERATOR</span>`
  (IBM Plex Mono 14 px bold caps, widest tracking). Right: the **Sign out** button
  (paper-outlined, POST form to `/operator/logout`). The band stays on top at every width.
- **Desktop (≥ 1200, `desk:`).** Under the band, a **light** left nav column, 232 px, paper
  (`#FFFDF4`) ground, 1 px `line` right border. A mono section label **"PLATFORM"** above five
  items: **Console · Tenants · Holdings · Audit log · Legal texts** (min-height 44 px, Space
  Grotesk 15 px, ink). Current item: ink 3 px left marker + `bg-ink/5` + bold +
  `aria-current="page"`. Content to the right on porcelain, max-width about 960 px.
- **Tablet (768–1199, `md:`).** Under the band, a horizontal row (paper, `line` bottom border):
  the same five items as text tabs, 44 px tall; current = ink 3 px bottom marker + bold; wraps if
  needed.
- **Phone (< 768).** The band stays (lockup + sign out). A **bottom tab bar in paper (not
  ink)** with a 1 px `line` top border; five items, icon 22 px + 11 px label: **Console, Tenants,
  Holdings, Audit, Legal**; current = ink icon + bold label + ink 3 px top marker; inactive
  label `text-ink/70`. The bar is **sticky** at the viewport bottom and, on a page shorter than
  the screen, still **sits on the viewport's bottom edge** (the polished boards): the frame is a
  flex column at least the viewport tall and the bar takes `margin-top: auto` — §6 "Frame" and
  "Main nav". Because a sticky bar keeps its place in the flow, it never covers the last control.
- **Icons** (`viewBox="0 0 24 24"`, `fill="none"`, `stroke="currentColor"`, `stroke-width="1.8"`,
  `stroke-linecap="round"`, `stroke-linejoin="round"`, `aria-hidden="true"`):
  - console `<rect x="3" y="4" width="18" height="14" rx="2"/><path d="M7 20h10M8 9l3 2-3 2M13 13h3"/>`
  - tenants `<path d="M3 21V9l6-4 6 4v12"/><path d="M15 21V13h6v8"/><path d="M7 13h4M7 17h4"/>`
  - holdings `<circle cx="7" cy="7" r="3"/><circle cx="17" cy="7" r="3"/><circle cx="12" cy="17" r="3"/><path d="M9.5 8.5l1.5 6M14.5 8.5l-1.5 6"/>`
  - audit `<path d="M6 3h9l4 4v14H6z"/><path d="M15 3v4h4M9 12h7M9 16h7"/>`
  - legal `<path d="M12 3v18M5 7h14M7 7l-3 7h6zM17 7l-3 7h6z"/>`
- **One-tenant screens** (overview, plaques, billing, VAT number): below the nav (desktop: at
  the top of the content column), the existing saffron-lite **tenant banner** with its **1 px**
  saffron bottom rule (label "Tenant", name in display XL bold; **"Unnamed tenant"** + the id,
  mono, when unnamed), and **inside the banner**, under the name, the **tenant sub-nav**
  `<nav aria-label="This tenant">`: **Overview · Plaques · Billing · VAT number** (text tabs on
  the banner's saffron-lite ground, 44 px tall, 3 px transparent bottom border sitting on the
  banner's rule; current = **saffron** 3 px bottom marker + bold + `aria-current="page"`; Space
  Grotesk 15 px on desktop and tablet, **14 px on the phone, where the row wraps** instead of
  scrolling). The main nav's **Tenants** item is current on these screens. "Back to the tenants"
  stays as a link. This is how all fifteen tenant-screen boards draw it; where an earlier draft
  of this README put the sub-nav on paper under the banner, the boards win (orchestrator
  decision, 2026-10-20).
- **DOM order the tests will see** (signed-in; one DOM for all widths):

  ```
  <header class="op-bar"> … sign-out form … </header>
  <div class="op-frame">
    <nav class="op-nav" aria-label="Operator sections"> … five links … </nav>
    <div class="op-col">                                   (content column)
      <section class="op-tenant" aria-label="Tenant">      (one-tenant screens only)
        <p class="docket-label">Tenant</p> <p …name…></p>
        <nav class="op-subnav" aria-label="This tenant"> … four links … </nav>
      </section>
      <main class="op-main …"> … the page … </main>
    </div>
  </div>
  ```

  The banner (with its sub-nav) is **outside and before `<main>`**, as it is today
  (`chrome.templ:91-99`), so `<header class="op-bar">` still precedes `<main`, the banner still
  precedes the page, and the tests that search `body[banner:]` for the name keep finding it. On
  the phone the `op-nav` is moved to the bottom by `order-last` (sticky, `mt-auto` — §6), so
  its early DOM position costs nothing visually and keeps it first for keyboard and
  screen-reader users. Signed-out screens:
  `<header class="op-bar">` (no form) then `<main class="op-main …">` — no frame, no nav.
- **Signed-out screens** (sign-in, code, enrollment, signed-out problems): the band **without**
  nav and **without** sign out; a centred single-column card.
- **Signed-in problem pages:** band + nav, **no** current item (a problem is not a section) —
  no `aria-current` in `op-nav` and no current marker at any width; the polished `OpProblem`,
  `T-OpProblem` and `M-OpProblem` signed-in states draw it so. No banner, no sub-nav (a problem
  page is not a one-tenant screen, even when it refuses a tenant's path).

**Why it stays unmistakable from the tenant panel** (the reason `chrome.templ` gave for having
no nav still holds — it is met another way):

| | Tenant panel ("The Pass", shipped) | Operator surface |
|---|---|---|
| Top | no band; green `taptime` wordmark, tenant logo, 4 px accent stripe | full-width **ink band** + 4 px **saffron** rule, mono caps lockup **TAPTIME OPERATOR** |
| Desktop nav | **ink** sidebar 248 px, `.panel-side`, `.side-nav` | **paper** column 232 px, own `op-*` classes, "PLATFORM" label |
| Tablet | 88 px icon **rail** | text-tab **row** under the band |
| Phone | bottom bar + `<details>` **More** sheet, `.bar-link` | paper bottom bar (sticky), five items always shown, **no** More sheet |
| Colour of "current" | tappa-green / tenant accent | ink (main nav), **saffron** (tenant sub-nav — the warning tone, because the screen shows another business) |
| Brand | tenant accent and logo | none — `Theme{}`, CSP without `img-src` |

The operator surface uses **its own `op-*` classes only** and none of the panel's
(`panel-side`, `side-nav`, `bar-link`, `tab-link`, `sheet-*`, `punchless`, the green
`taptime` wordmark, a dark sidebar).

### OP-2 — Breakpoints: desktop + tablet + phone

Board stems `Op<Name>` (desktop 1440, fluid), `T-Op<Name>` (834), `M-Op<Name>` (390). Twelve
screens: OpSignIn (with the Code step as a state), OpEnroll, OpHome, OpProblem, OpTenants,
OpTenant, OpPlaques, OpBilling, OpVat, OpAudit, OpHoldings, OpLegal. Tailwind breakpoints are the
panel's: base = phone, `md:` = 768, `desk:` = 1200 (`tailwind.config.js` `screens.desk`). Each
screen card ports all three widths together — **one DOM**, restyled by breakpoint, never two
copies of a control.

---

## 3. Constraints every operator screen inherits

The panel README §3 applies where it is generic (Tailwind scans only `.templ` and
`web/static/js`, so no class name is built in Go and no utility name is written in a templ
comment; Plex Mono 400/700; touch targets ≥ 44 px; status never colour alone; no float for
money). The operator-specific rules:

1. **CSP** (`render.go:28-29`): `default-src 'none'; style-src 'self'; font-src 'self';
   form-action 'self'; base-uri 'none'; frame-ancestors 'none'`. There is **no `img-src`** — no
   `<img>`, no logo, no `data:` URI in CSS, no CSS background image. Icons and the QR are
   **inline SVG** with presentation attributes. Enrollment alone adds `script-src` naming
   `/static/js/operator/enroll.js` (`render.go:39-41`). Every response also carries
   `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, nosniff
   (`TestOperatorHeaders_FortyResponseClassesCarryThePolicy` and the per-screen
   per-screen operator header tests, `…ClassesCarryThePolicy`, e.g. `TestOperatorHeaders_TheAuditClassesCarryThePolicy`).
2. **No new JavaScript.** Every action is a POST form, selection is a link, disclosure would be a
   native `<details>`. No htmx on this host. The nav needs none: it is five links.
3. **`Theme{}` — no tenant brand.** No `brand` class or variable, no accent, no theme link, no
   logo. A class name containing `brand` also breaks `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`.
4. **Tenant name banner** (ADR 0020 §9): every one-tenant screen names the tenant in the header.
   `TenantScreen` refuses to render without a name (`view.go:91-98` → plain 500;
   `TestTenantScreen_RefusesToRenderWithoutAName`). Platform-wide screens (Home, Tenants, Audit,
   Holdings, Legal) have no banner; each row names its tenant.
5. **Home makes no read** (`home.templ:7`, `console.go:12-23` — `home` at :15 renders `Home()` and calls no store). A figure on Home would be a
   new audited read and a new `op_*` (a migration, ADR 0021) — not a port.
6. **Read budget.** Each data screen view = one `read` audit row + one budget unit
   (`TestReadBudget_TheReadsOfEveryScreenShareOneBudgetPerSession`; per screen
   `TestTenantPages_AReadCountsTwiceAgainstTheSessionBudget`, `TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget`,
   `TestBillingBudget_EachViewIsOneReadOfTheSessionsSharedBudget`,
   `TestAuditBudget_EachViewIsOneReadOfTheSessionsSharedBudget`,
   `TestHoldingsBudget_AViewIsOneReadAndAWriteIsNone`). **No combined screens** (an overview with
   inline plaques), no auto-refresh, no widget anywhere.
7. **Package boundary.** `operatorpages` may import only `web/templates/components` and
   `web/templates/layout`, and only `internal/handler/operator` imports it
   (`TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell`). It cannot use the
   panel's `pages.PanelNotice` / `PanelProblem`. `components.Notice` and every `layout.*` are
   shared with the tap / activation screens and byte-pinned (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`)
   — **operator-only components live inside `operatorpages`, unexported**. Unexported matters:
   `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` pins the package's exported
   component constructors to the fourteen screens (`typepins_test.go:1377`).
8. **Designed egress only** (`TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`,
   `leak_test.go`). The leak test searches logs, bodies and headers for passwords, TOTP code and
   secret, enrollment token, session token, challenge, pending blob, operator e-mail, client
   address, legal text, search term, VAT number and holding name. Allowed places: key/QR/URI/blob
   on the enrollment first load; blob + link token on the enrollment re-render; legal texts on
   Legal; the search term on its own result page; the VAT number on the VAT screen; holding names
   on the holdings list. **Nothing else** — no operator e-mail in the band, no VAT number on the
   overview, no "recent searches", no echo of a refused name or address.
9. **POST bodies stay bodies.** Tenant search and its pager, the audit filter and pager and the
   billing pager are POST forms; never turn them into `?q=` links
   (`TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm`, `TestAuditScreen_ReadsNothingFromTheURL`,
   `TestBillingScreen_ReadsNothingFromTheURL`). The holdings pager is a link by design.
10. **Links and actions are same-origin mounted paths** (no `://`) —
    `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`, `TestProblemPages_LinkOnlyToMountedRoutes`.
    The customer site's `/legal` stays **text**, not a link. Do not draw or link the deliberately
    unmounted paths (`TestSurface_TheScreensOfLaterTasksAreNotMounted`: `/operator/billing`,
    `/operator/plaques`, `/operator/vat`, `/operator/tenants/{id}/audit`, …).
11. **§4.6 — never 0 for something not measured.** Billing's unreadable and before-sign-up rows
    show no figure ("not even a zero"); a failed read is a problem page, never an empty list.
12. **SVG budget.** `TestEnrollQR_OnlyOnTheFirstLoad` counts `<svg` on every fixture render: 1 on
    the enrollment first load, **0 everywhere else** today. The nav's five icons change that
    count on signed-in renders — the test is **extended** in M13-01 (§5), never loosened, and
    the enrollment page (signed out) still carries exactly one SVG: the QR.
13. **Touch targets.** Every new pressable `op-*` class carries `min-h-11`; the input.css check in
    `TestAuditScreen_WearsTheDocketAnatomy` (`op14_test.go:750`, the `op-input`/`op-link` list) is
    extended with the new classes (`op-nav-item`, `op-subnav-item`, `op-pager`).
14. **Red-line touchpoints.** CLAUDE.md §4.5 exception (1) is this surface: **no new `op_*`, no
    widened return column.** Holding membership is written only by `op_*` here (ADR 0027 §2) —
    no rename/delete holding. The VAT number is not editable.
15. **Tests during a task:** only changed tests with `-run` + `gofmt`, build,
    `bash scripts/redline-check.sh`; the full suite and `make check` once, before the commit
    (CLAUDE.md §10). `make gen` after every `.templ` edit; generated files are committed.

---

## 4. Tests that pin chrome, markup and copy

A port that changes pinned markup updates the test **in the same commit**, keeping every
assertion's intent; nothing is loosened to turn a test green.

### 4.1 The chrome — `TestOperatorScreens_EveryOneWearsTheOperatorChrome` (`op8_test.go:760-796`)

Walks the 46 fixture renders of `screens()` (`op8_test.go:550`), 37 of them signed in, and
requires on **every** render:

- `<header class="op-bar">` **before** `<main` (so nothing may move the band below the page, and
  the nav sits between the two);
- the exact lockup `TAPTIME <span class="text-saffron">OPERATOR</span>`;
- none of `panel-side`, `side-nav`, `bar-link`, `tab-link`, `punchless`, `text-tappa-green">taptime`;
- the sign-out form `action="/operator/logout"` present **exactly** on the 37 signed-in renders;
- a title ending `Taptime operator</title>`.

**How M13-01 extends it (never weakens it):** all five existing checks stay byte-for-byte;
added per render —

- signed-in renders carry **one** `<nav class="op-nav"` (single DOM for all three widths)
  between the band and `<main`, with exactly the five hrefs `/operator`, `/operator/tenants`,
  `/operator/holdings`, `/operator/audit`, `/operator/legal`; signed-out renders carry **none**;
- `aria-current="page"` in the main nav: exactly one on every signed-in render except
  "problem, signed in" (zero), and it is the right item (Console on home; Tenants on the tenant
  list and the four one-tenant screens; Holdings, Audit log, Legal texts on theirs);
- **every one-tenant render (17** — the 16 overview / plaques / billing / VAT fixtures plus the
  bare "tenant screen" fixture**)** carries the tenant sub-nav **inside** the banner
  (`<section class="op-tenant"` … `<nav class="op-subnav"` … `</section>`, before `<main`) with
  the tenant's four paths and exactly one current (Overview / Plaques / Billing / VAT number;
  the bare fixture names which one it passes); no other render carries it;
- the panel-marker list grows by the shell classes shipped since (`sheet-`, `panel-well`,
  `panel-stripe`) and the tenant-brand markers (`/brand/` — the theme stylesheet and logo paths,
  `layout/theme.go:41-46` —, `bg-brand`, `--brand`, `<img`); not the bare words "brand" or
  "accent" (the weak-password notice says "a letter with an accent", `enroll.templ:71`)
  — or that part lives in a **new** test next to it (its name is the builder's; do not cite a test
  name before it exists — `TestEveryNamedTestExists`).

### 4.2 Other tests a port must keep green (or update deliberately)

| What | Test(s) | Pins |
|---|---|---|
| Links and actions | `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` (`op8_test.go:856`) | Home's exact `<a href="/operator/holdings" class="op-link">Holdings</a>` and `…/audit" class="op-link">Audit log</a>`; form **counts** on overview (open 1, close 1), holdings, audit (3 when filtered), billing page 3 (2), VAT (1, 0 for an odd number). The counts match **exact opening-tag prefixes, attribute order included** — e.g. `<form method="post" action="/operator/tenants/<id>/integration" class=`, `<form method="post" action="/operator/holdings" class=`, `<form method="post" action="/operator/tenants/<id>/billing">` — so a port keeps `method` before `action`, and keeps or omits the trailing `class=` exactly as today. Every `action`/`href`/`src` must be a mounted same-origin path. The nav adds links, no forms. |
| One form, one button on plaques | `op13_test.go:327-332` (in `TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible`) | the plaques page has exactly **one** `<form` (the sign-out, `<form method="post" action="/operator/logout">`), exactly **one** `<button`, and no `hx-` attribute. So sign-out is one form and one text button at every width; the nav and sub-nav hold neither. |
| SVG count | `TestEnrollQR_OnlyOnTheFirstLoad` (`enrollqr_test.go:202-229`) | 1 SVG on enroll first load, 0 elsewhere → extended in M13-01 |
| QR | `TestEnrollQR_IsTheKeysURIInkOnPaper` | `class="op-qr"`, `role="img"`, aria-label, `#152219`/`#FFFDF4`, order svg → key → uri |
| Script | `TestEnrollScript_IsTheReviewedBody`, `TestEnrollScript_TouchesTheFragmentAndNothingElse` | ids `enroll-token`, `enroll-token-wrap`; the script's body |
| Banner | `TestTenantScreen_RefusesToRenderWithoutAName`, `TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath`, `TestTenantOverview_AnUnnamedTenantIsNamedByItsID` | `<section class="op-tenant" aria-label="Tenant">`, `<bdi>name</bdi>` after it, `<p class="font-display text-xl font-bold tracking-tight">Unnamed tenant</p>` + `<p class="break-all font-mono text-sm">id</p>`, title `<Title> — <name> — Taptime operator` |
| Docket anatomy | `TestAuditScreen_WearsTheDocketAnatomy` (`op14_test.go:708`), `TestBillingScreen_WearsTheDocketAnatomy` (`op12_test.go:1411`) | single `section.docket` with `aria-labelledby`, `<li class="op-row">`, heading markup, `<select id="audit-kind" name="kind" class="op-input">`, `<button type="submit" class="op-link">…</button>` pager (`op14_test.go:739-740`, `op12_test.go:1441`; also `TestBillingScreen_PagesAreFiveOfTwelve`'s form at `op12_test.go:1370-1373`), billing's labelled facts `<span class="text-sm">LABEL <span class="font-mono">…</span></span>` (`op12_test.go:1436-1440`) → the pager class and the billing receipt strip change these **in the same commit** (§6 "POST pager", "Billing receipt strip") |
| Chips | `TestTallyRules_NoToneRuleColoursTheWord`, `TestCompiledCSS_TallyWordIsInkOnEveryChip`, `Test…Screen_EachChipHasTheRuleTheContrastTestComputes` | chip regex `<span class="tally (tally--[a-z-]+)">` — **no extra class on the span**; billing's rule test wants exactly a ground and a frame per tone (`op12_test.go:1532-1583`) → the dashed live tone extends it (§6 "Chip / tally") |
| AA | `TestOperatorChrome_TheColouredTextClearsAA`, `TestTenantScreens_TheTextClearsAA`, `TestLegalScreen_TheTextClearsAA`, `TestAuditScreen_TheTextClearsAA`, `TestBillingScreen_TheChipsAndTextClearAA`, `TestPlaqueScreen_TheChipsAndTextClearAA`, `TestVATScreen_TheChipsAndTextClearAA` | contrasts recomputed from `tailwind.config.js` |
| View fields | `TestPlaqueScreen_NoKeyReachesThePage`, `TestBillingScreen_OffersNoItemisationAnywhere`, `TestAuditScreen_NoCredentialFieldReachesThePage` | the view types' field lists |
| Copy | `TestBillingScreen_EveryStateSaysItsWordAndNoVerdict`, `TestVATScreen_EveryStateSaysItsWordAndSentence`, `TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem`, `TestPlaqueWords_NameEveryShapeAndNothingElse`, `TestAuditWords_NameEveryKindAndNothingElse`, `TestEnroll_TheWeakPasswordNoticeNamesBothLimits`, `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` | sentences and words verbatim |
| Legal notice | `op10_test.go:492` (`warnNotice` regex) | `<section class="border-l-4 border-saffron…">` — the "public page is behind" notice; updated in M13-03 when the notice changes |
| Sign-in sameness | `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike` (operatorauth) | every refused sign-in answers byte-identically |

---

## 5. Token map

Use the panel README §4 unchanged: palette (`ink #152219` · `porcelain #EDF0EA` · `paper #FFFDF4`
· `tappa-green #1F5C41` · `green-lite #E1EDE6` · `saffron #D98E2B` · `saffron-lite #F7EBD6` ·
`tomato #BE3D2A` · `line #C9D2C8`), opacity steps, rgba → class table, radius table (D2), type
sizes. Operator-specific mappings from the boards:

| Board value | Class | Where |
|---|---|---|
| `rgba(21,34,25,.05)` | `bg-ink/5` | current main-nav item, nav hover |
| `rgba(21,34,25,.7)` | `text-ink/70` | phone bar inactive label (6.05:1 on paper) |
| `rgba(217,142,43,.1)` | `bg-saffron/10` | tenant sub-nav hover |
| `#D98E2B` 3 px | `border-b-[3px] border-saffron` | tenant sub-nav current marker (the banner's own rule is 1 px `border-b border-saffron`) |
| `#F7EBD6` | `bg-saffron-lite` | tenant banner, and the sub-nav row inside it |
| `#152219` 3 px | `border-l-[3px]` / `border-b-[3px]` / top marker `before:` | main-nav current marker (desk / tablet / phone) |
| card radius 14 px | `rounded-[14px]` | `.op-card` (D2: cards may round; dockets stay sharp) |
| button radius 10 px | `rounded-[10px]` | sign out, primary and quiet buttons, fields |

---

## 6. Operator component catalogue

All new components are `@layer components` classes in `web/static/css/input.css` (beside the
existing `op-*` block at `input.css:1098-1145`, re-read at `a8a06f6`, unchanged at `27516b7`) and **unexported** templ components in
`web/templates/operatorpages/`. Breakpoints: base = phone, `md:` = tablet, `desk:` = desktop.

| Component | Extends / new | Recipe | Breakpoints · notes |
|---|---|---|---|
| **Band** `op-bar` | existing `.op-bar`, `.op-lockup`, `.op-signout` (`input.css:1098-1104`), `bar(signedIn)` (`chrome.templ:33-46`) | keep `<header class="op-bar">` and the lockup markup byte-for-byte; inner `flex min-h-16 items-center justify-between gap-3 px-4 md:px-6 desk:px-8` (full width now — it no longer sits in `max-w-3xl`); `.op-signout` gains `rounded-[10px]` | same at every width; one sign-out form. On the phone it stays the **text** button "Sign out" — no icon, no `<svg>` (the plaques page allows exactly one `<button>`, and the SVG count on signed-in renders is exactly the nav's five); `action="/operator/logout"` appears once. |
| **Frame** `op-frame` | new | `flex min-h-[calc(100vh-4.25rem)] flex-col desk:flex-row` (4.25rem = the band's 64 px row + its 4 px rule, so band + frame fill the viewport; `layout.Operator`'s `<body>` is shared and stays untouched); content column `min-w-0 flex-1` | wraps nav + content column on signed-in screens only. **No `overflow-*` on the frame or any ancestor of the nav** — it would break the phone bar's `sticky`. |
| **Main nav** `op-nav` | new; **one** `<nav class="op-nav" aria-label="Operator sections">`; links only — **no `<form>`, no `<button>`** inside (the plaques page allows exactly one of each, the sign-out) | desk: `desk:static desk:w-[232px] desk:flex-none desk:flex-col desk:border-r desk:border-line desk:bg-paper desk:px-3 desk:py-6`; label `.op-nav-label` `hidden desk:block px-3 pb-2 font-mono text-[11px] uppercase tracking-[0.12em] text-ink/70` "PLATFORM"; item `.op-nav-item` `flex min-h-11 items-center gap-2 font-display text-[15px] text-ink hover:bg-ink/5`; current `aria-[current=page]:font-bold` + marker | **tablet** `md:static md:order-first md:mt-0 md:flex md:flex-row md:flex-wrap md:border-t-0 md:border-b md:border-line md:bg-paper md:px-2.5 md:pb-0` (the `md:static`, `md:order-first`, `md:mt-0`, `md:border-t-0` and `md:pb-0` undo the phone's sticky bottom bar; desk adds `desk:order-none`), item `md:min-h-11 md:flex-row md:px-3.5 md:text-[15px] md:border-b-[3px] md:border-transparent md:aria-[current=page]:border-ink`; **desk** item `desk:border-b-0 desk:border-l-[3px] desk:aria-[current=page]:border-ink desk:aria-[current=page]:bg-ink/5`; **phone** `sticky bottom-0 z-10 order-last mt-auto grid grid-cols-5 border-t border-line bg-paper pb-[env(safe-area-inset-bottom)]` — sticky, not `fixed` (polished boards): in the frame's flex column, `order-last` puts it under the content and `mt-auto` pins it to the viewport's bottom edge on a short page; on a long page it sticks while scrolling and comes to rest under the last control, item `relative min-h-[60px] flex-col justify-center gap-0.5 text-[11px] text-ink/70 aria-[current=page]:text-ink aria-[current=page]:font-bold` with an ink 3 px top marker on current. Icon `<svg class="size-[22px] md:hidden" aria-hidden="true">`. Phone label vs wider label: `<span class="md:hidden">Audit</span><span class="hidden md:inline">Audit log</span>` (and Legal / Legal texts). **Not** the panel's sidebar classes. |
| **Content column** | new wrapper `div.op-col` (`min-w-0 flex-1`) + `<main>` replacing `<main class="mx-auto … max-w-3xl … px-4 py-8">` (`chrome.templ:72,83,95`) | `<main class="op-main mx-auto flex w-full max-w-[960px] flex-col gap-4 px-4 pt-4 pb-6 md:gap-5 md:px-6 md:py-6 desk:gap-6 desk:px-10 desk:pt-8 desk:pb-14">` — the **`op-main`** class is introduced here (M13-01) because the chip rule (M13-02) scopes to it | phone bottom padding only for breathing room above the bar (the sticky bar sits in the flow, so the old `pb-24` clearance for a `fixed` bar is not needed: `pb-6`, or the gap the M- boards draw); signed-out screens: `op-main` too, centred `max-w-lg`, no bottom padding |
| **Tenant banner** `op-tenant` | existing `.op-tenant` (`input.css:1105`), `tenantBanner` (`chrome.templ:54-66`) | keep `<section class="op-tenant" aria-label="Tenant">`, the label `docket-label` "Tenant", the two `<p>` shapes and `<bdi>` exactly; rule stays **1 px** `border-b border-saffron` (as today and as drawn); inner `max-w-[960px] px-4 pt-4 md:px-6 md:pt-5 desk:px-10 desk:pt-6` with **no bottom padding** — the sub-nav row inside it sits on the rule; name `md:text-2xl desk:text-3xl` is allowed **only** if the pinned `<p class="font-display text-xl font-bold tracking-tight">` stays (add the size via a parent selector rule, not on the `<p>`) | in the content column on desktop, full width under the tab row on tablet, under the band on phone |
| **Tenant sub-nav** `op-subnav` | new; `<nav class="op-subnav" aria-label="This tenant">` **inside** `section.op-tenant`, after the name | `mt-2 -ml-2.5 flex flex-wrap` (no ground of its own: the banner's saffron-lite); item `.op-subnav-item` `-mb-px inline-flex min-h-11 items-center whitespace-nowrap border-b-[3px] border-transparent px-2.5 font-display text-sm md:px-3.5 md:text-[15px] text-ink hover:bg-saffron/10 aria-[current=page]:border-saffron aria-[current=page]:font-bold` | phone 14 px and **wraps** (as `M-Op*` draw it); tablet/desk 15 px. Ink on saffron-lite is AA (ink/70 there is already 5.6:1). Paths from the view (`OverviewPath`, `PlaquesPath`, `BillingPath`, `VATPath`) — content, not classes. |
| **Card** `op-card` | existing `.op-card` (`input.css:1106`) | `rounded-[14px] border border-line bg-paper p-5 md:px-6 md:py-[22px] flex flex-col gap-3.5`; drop `shadow-sm`. **Padding alignment (polished boards):** phone card **20 px** all round (`p-5`); tablet/desktop cards 22 px 24 px (`md:py-[22px]`) — 24 px sides, so a card's text and a docket's text start on one vertical line | label `docket-label` (pinned on the overview `dt`s) or `.op-label` (`font-mono text-[11px] uppercase tracking-[0.12em] text-ink/70`) for new eyebrows; h1 `font-display text-2xl md:text-[28px] desk:text-[32px] font-bold tracking-tight` — **one** `<h1>` per page |
| **Docket list** | existing `.docket` (`input.css:132-142`, sharp, perforated) + `.op-row` / `.op-version` (`input.css:1142-1145`) | keep `<section class="docket" aria-labelledby="…">` and `<h2 id="…" class="docket-label">` exactly; padding by a **scoped** rule (`.docket` is the panel's too and its `px-4 py-3` stays): `.op-main .docket { @apply px-5 py-4 md:px-6 md:py-[18px]; }` — phone **16 px 20 px**, tablet/desktop **18 px 24 px** — **one scale and one look on every operator screen**: the perforated `.docket` everywhere (the boards' dashed-top shortcut on Tenants / Tenant / Plaques / VAT is being aligned to it) and this single padding scale for every docket and card; no per-screen padding; row `flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-dashed border-line py-2.5 last:border-b-0`; first fact mono bold; facts `text-sm`; ids `break-all font-mono text-xs text-ink/70` | **sharp** (D2). Desk: facts on one line where they fit; phone: wrap. `<li class="op-row">` / `<li class="op-version">` stay literal (regex-pinned). |
| **Chip / tally** | existing `.tally` + tones (`input.css:678-756`) | `rounded-full` via a **scoped** rule `.op-main .tally { @apply rounded-full px-2.5; }` (`op-main` is put on `<main>` by M13-01's content column), never by adding a class to the span. **`TestTallyRules_NoToneRuleColoursTheWord` reads every rule whose selector names `.tally`** (`readTallyRules`, `op12_test.go:1645-1707`) and allows only the base `.tally` and lists of `.tally--x` whose `@apply` is frames and grounds — so a `.op-main .tally` selector is a finding today. M13-02 extends the reader with exactly one more allowed shape, the scoped `.op-main .tally` rule, whose utilities may be only shape and type ones (`rounded-*`, `px-*`, `py-*`, `min-h-*`, `font-bold`, `tracking-*` — the boards' chip is 24 px tall, 2 px 10 px, bold, tracking .08em) — no `text-*`, `bg-*`, `border-<colour>`, `opacity` — with a mutation control (a colour utility added to the scoped rule is red). (Re-read at `a8a06f6`, unchanged at `27516b7`: the test is **already red** on the panel's `.tally--pill`, `input.css:1292`, added in `2afa9db`; its fix is the panel card **M12-52**, a precondition of M13-02. M13-02 must never be satisfied by widening what a `.tally--x` rule may apply; its reader extension is only the selector exactly `.op-main .tally`, one `@apply`.) | the span stays `<span class="tally tally--x">` (regex-pinned); word ink, colour in the frame. **One re-toned modifier, no new one:** the polished billing boards draw a live month (`tally--running`, written only by `billing.templ`) as a **dashed saffron outline** — its selector leaves the line group (`input.css:731-736`) for its own one-line rule `.tally--running { @apply border-dashed border-saffron bg-paper; }` (boards' `.t-saffron-o`: a dashed saffron frame on **paper**). The modifier name still matches `tally--[a-z-]+`, so the chip regex is unchanged; M13-02 (where the CSS changes) extends the two billing tests in the same commit — the rule test allows exactly `border-dashed` as a third token for this tone only, the AA test names the running month on its existing ink-on-paper line (16.17:1) instead of the line/10 one. The other billing tones already match the boards (ended-not-closed saffron, frozen green, before sign-up line, unreadable ink). |
| **Fact grid** (`dl`) | existing overview / VAT `dl` | `grid grid-cols-1 gap-4 md:grid-cols-2 desk:grid-cols-4`; big counts keep `<dd class="mt-1 font-mono text-3xl font-bold">` (regex-pinned) | phone 1 col (facts) / 2 col (counts) |
| **Field** `op-input` | existing `.op-input` (`input.css:1107-1110`) | add `rounded-[10px]`; label `op-label`; help `text-[13px] leading-[1.45] text-ink/70` | keeps `min-h-11` (op14 input.css check). Textarea (Legal) and select (Audit) use it too. |
| **Buttons** | existing `btn btn--primary`, `btn btn--quiet` | `rounded-[10px]`; primary green, quiet paper + `line` border (tomato on hover — Close the API, Remove) | one button per form; no confirm step exists (D1) |
| **Link** `op-link` | existing (`input.css:1111`) | unchanged colour (tappa-green on paper 7.73:1); keeps `min-h-11` | "Back to …" links stay as drawn |
| **Inline link hit area** (audit tenant link; also the holdings member link) | new **scoped** rule, no markup change: the audit row's tenant link is `<span class="text-sm">Tenant <a href=… class="op-link">…</a></span>`, pinned byte-for-byte (`op14_test.go:317, 328, 387`) | as the boards' `.rowlink` (`OpAudit`, `OpHoldings`): the link keeps the line's height and gets a **44 px invisible overlay** — `[aria-labelledby="audit-list"] .op-row > span > .op-link { @apply md:relative md:inline md:min-h-0; }` + `…::after { content: ''; @apply md:absolute md:inset-x-0 md:top-1/2 md:h-11 md:-translate-y-1/2; }` (transparent, no colour; the boards draw it at every width — on the phone `op-link`'s own `min-h-11` already gives 44 px, so the scoped rule may stay `md:`) | `.op-link`'s rule itself keeps `min-h-11` (the op12/op14 input.css checks). |
| **Search form** | `Tenants` form (`tenants.templ:23-55`) | `<form role="search" method="post" action="/operator/tenants">`; field + button in `flex flex-col gap-2 md:flex-row md:items-end` | POST only; the term never becomes a URL |
| **Pager** `op-pager` | **new** class; `auditPageForm` (`audit.templ:195-201`), `billingPageForm` (`billing.templ:129-134`), `holdingPager` (`holdings.templ:165-176`) | the polished boards draw Billing's **Newer months / Older months**, Audit's and Holdings' **Previous page / Next page** as **outlined panel-style buttons** (boards' `.pgbtn`): `.op-pager` `inline-flex min-h-11 items-center justify-center whitespace-nowrap rounded-[10px] border border-ink/30 bg-paper px-[22px] font-display text-[15px] font-bold text-ink no-underline hover:border-ink`; the `<nav aria-label="…">` `flex flex-wrap gap-3` (boards' `.pager`) | Audit and Billing stay POST forms (`<form method="post" action=…>` + hidden fields + `<button type="submit" class="op-pager">`); Holdings stays **links** (`<a href="/operator/holdings?page=N" class="op-pager">`). The class change is made **in the tests in the same commit** — `op14_test.go:739-740` (audit), `op12_test.go:1370-1373` and `:1441` (billing) — keeping every structural assertion (element `button`, `type="submit"`, form `method`/`action`, hidden `page`/`kind` fields, label) and adding a control that the old `class="op-link">Previous page</button>` / `Older months</button>` is **absent**, so the new string is not matched alongside a leftover. `.op-pager` joins the 44 px input.css check (`op14_test.go:750`). Ink/30 on paper is a frame, not text (the word is ink, 16.17:1). The tenant list's pager (`tenantPageForm`) is not in the polished list: it keeps `op-link` unless its board is redrawn. |
| **Operator notice** `opNotice` | **new, unexported** `opNotice(tone, heading)` in `operatorpages/notice.templ`; **do not** edit `components.Notice` | ok `rounded-[14px] border border-tappa-green bg-tappa-green/10 px-[18px] py-4`; warn `border-saffron bg-saffron-lite`; alert `border-2 border-tomato bg-paper`; heading stays a `<p class="font-display font-bold">` (M13-03: three test helpers read `>heading</p>`); body `mt-1 text-sm text-ink/85`; `role="status"` (ok, warn) / `role="alert"` (alert) | headings stay the code's (e.g. "Opened.", "Not added — nothing changed."); rules lists as `<ul class="list-disc pl-5">`. Callers: SignIn, Code, Enroll, Holdings, TenantOverview API, VAT, Legal, Problem. |
| **Problem page** | `Problem` (`home.templ:53-65`) | inside the frame: `opNotice(alert)` with the `<h1>` = Title and the message; optional `op-link` Back | signed-out: centred card, no nav; signed-in: nav with no current. No status chip (the view carries no status; D1). |
| **Signed-out card** | `SignIn`, `Code`, `Enroll` | `mx-auto w-full max-w-lg`; card `.op-card` | band without nav or sign out |
| **QR + key** | existing `.op-qr`, `.op-key`, `.op-uri`, `.op-token-field` (`input.css:1114-1130`) | unchanged; the QR is the only SVG on the enrollment page | order svg → key → uri pinned |
| **Step / card heading** `op-step`, `op-heading` | **new** classes (names the builder's) for the two kinds of in-card heading the polished boards draw where the code writes `docket-label` | `.op-step` `font-display text-base font-bold tracking-tight md:text-[17px]` — Enroll's "Step 1 · …" / "Step 2 · …" (16 px phone, 17 px tablet/desktop); `.op-heading` `font-display text-[19px] font-bold tracking-tight` — Holdings' "Create a holding" | same words; the element may become an `<h2>` (still one `<h1>` per page); Holdings keeps `id="holding-create"` (the card's `aria-labelledby`). No test pins `docket-label` on these three (re-read at `a8a06f6`, unchanged at `27516b7`). |
| **Billing receipt strip** | **new**, replaces the four inline figure spans of `billingRow` (`billing.templ:68-71`) | as the boards' `.figs`: a `<dl class="op-receipt">` of four cells, each `<div><dt class="docket-label">Plan</dt><dd class="font-mono">…</dd></div>` — **PLAN · PEOPLE COUNTED · PRICE PER PERSON · AMOUNT** (10 px mono caps label above the value; the words stay the code's, the caps are CSS); strip `grid grid-cols-2 gap-x-5 gap-y-3 border-y border-dashed border-line py-3 md:grid-cols-[repeat(3,minmax(0,1fr))_minmax(0,1.2fr)]`; values `font-mono text-base`; the Amount cell `text-right` (items at the end), its value `text-xl font-bold` (20 px) | phone 2 × 2. Closed / First charged month / Counted over stay `text-sm` spans. Tests updated in M13-10. |

### 6.1 Screen recipes from the polished boards (2026-10-20)

Look only — every word, form and hidden field stays the code's (`screens.md`); pinned markup
named beside each recipe stays byte-for-byte. Selectors that must not touch a pinned tag are
**scoped** from a parent (`<li class="op-row">` and `<li class="op-version">` stay literal).

| Screen | Recipe | Pinned — keep |
|---|---|---|
| **Home hub** (M13-06) | **one** `op-card` (as the code: `home.templ:22-47`), on the single card scale (§6 "Card"): label `Operator console` mono (`docket-label` as today, or `op-label`); `<h1>` **28 px at every width** (`text-[28px] leading-[1.1]`); the list `mt-[18px] border-t border-line`, each `li` a row with a **dashed** bottom rule (`border-b border-dashed border-line`): on tablet/desktop `md:grid md:grid-cols-[128px_minmax(0,1fr)] md:items-baseline md:gap-x-4 md:py-1` (link left, gloss right), on the phone `flex flex-col items-start pt-0.5 pb-3` (the link — `op-link`, 44 px — above its gloss); links 17 px, gloss `text-sm text-ink/70`; the session sentence is the card's last paragraph, **inside the card**, `mt-[18px] text-sm text-ink/70` (no rule) | the four `<a href=… class="op-link">` exactly (`op8_test.go:856` pins Holdings and Audit log) — the 17 px size comes from a parent rule, not a class on the `<a>`; the gloss keeps its leading "— " (code content). No icon tile, no docket. |
| **Enroll steps** (M13-05) | "Step 1 · …" / "Step 2 · …" as `.op-step` headings (16 / 17 px, §6) instead of `docket-label`; the QR block under step 1 unchanged | `<svg class="op-qr" role="img" aria-label=…><title>…</title>` and svg → key → uri order (`TestEnrollQR_IsTheKeysURIInkOnPaper`) |
| **Tenants row** (M13-07) | tablet/desktop, one line per tenant: `[aria-labelledby="tenant-list"] .op-row { @apply md:grid md:grid-cols-[minmax(0,1fr)_auto_76px_auto] md:items-center md:gap-x-5 md:py-1.5; }` — name link · Since … · plan (mono) · id (mono 12 px, `text-ink/70`, right-aligned, no wrap); phone: the name link alone on its first line, 44 px tall (`op-link`'s `min-h-11`), then Since and plan on one wrapping line, the id under them. The phone board puts a "·" between Since and plan — draw it with a CSS gap or `::before`, not a new character in the markup (D1) | `<li class="op-row">`; `op11_test.go:997`'s `<a href=… class="op-link"><span class="italic">Unnamed tenant</span></a>` |
| **Plaque row** (M13-09) | tablet/desktop (boards' `.plq`): two columns, `168px minmax(0,1fr)`, gap 4 px 24 px, row padding 16 px — the UID in the first column, everything else beside it: wrap chip, sentence and facts in **one** `<div>` after the UID span; `[aria-labelledby="tenant-plaques"] .op-row { @apply md:grid md:grid-cols-[168px_minmax(0,1fr)] md:gap-x-6 md:gap-y-1 md:py-4; }`; phone: stacked, UID (15 px bold, `break-all`) and chip on one wrapping head line | the row still **starts** with `<span class="font-mono font-bold">UID</span>` (`op13_test.go:285`); still no `<form`/`<button` beyond sign-out (`op13_test.go:327-332`) |
| **VAT "On file"** (M13-11) | the two facts side by side on tablet/desktop: `dl` (`vat.templ:41`) `flex flex-col gap-3 md:grid md:grid-cols-[max-content_minmax(0,1fr)] md:gap-x-14 md:gap-y-3`; stacked on the phone; the sentence and the time line under them, full width | `<dd class="mt-1 break-all font-mono text-lg font-bold">` (`op16_test.go:1409`) |
| **Holdings** (M13-13) | "Create a holding" as `.op-heading` (19 px) — `<h2 id="holding-create">` keeps its id. Member row (boards' `.mrow`): desktop a grid `minmax(0,1fr) 172px 212px 96px` — name cell (name link + chip, the tenant id **under** them) · Timezone + API code · Joined / Left · Remove; phone one column. The board nests the id inside the name cell; the port keeps the **code's DOM order** (`holdings.templ:131-160`: name, chip, Timezone, API code, Joined, [Left], id, [Remove]) and gets the same picture with order-preserving wrappers and a second grid row: `<div>` name + chip, `<div>` Timezone + API code, `<div>` Joined + Left, then the id span and the Remove form as they are, under `grid-template-areas: "name tz when act" "id tz when act"` (desktop) / `"name" "id" "tz" "when" "act"` (phone) — the `ol` around the members gains an `op-members` class to scope it. No test pins the member row's DOM order (re-read at `a8a06f6`, unchanged at `27516b7`). The member link can take the audit link's 44 px overlay (§6 "Inline link hit area") | `<span class="tally tally--active">Live</span>` / `…--deactivated">Ended</span>` (`holdings_test.go:689-690, 987`); form-count prefixes (`op8_test.go:884-888`); `href="/operator/holdings?page=2"` (`op8_test.go:890-891`, `holdings_test.go:741, 757`) |
| **Billing row** (M13-10) | month + chip on the first line; sentence; the **receipt strip** (§6) for the four figures; then Closed / First charged month / Counted over and the free-window / floor sentences as today | `<li class="op-row">`, `^\s*<span class="font-mono font-bold">Month` (`op12_db_test.go:168`) |
| **Audit row** (M13-12) | operator and account names in **Space Grotesk** (the code writes them in `<bdi>` with no mono — `audit.templ:92, 98`); mono only where the code prints it: time, page, session, ids, document slug, bytes; the inline tenant link gets the 44 px overlay (§6 "Inline link hit area") on tablet/desktop | `op14_test.go:317, 328, 387` tenant-link strings |
| **Legal editor** (M13-14) | **one full-width `op-card` per document** (as the code: `legal.templ:47-73`), the card itself without padding, split in bands (boards' `.ed*`) whose side inset is the single card scale — 24 px on tablet/desktop, 20 px on the phone (§6 "Card"; no per-screen padding). **Header** `flex flex-wrap items-center justify-between gap-x-5 gap-y-1.5 border-b border-line pt-5 pb-4`: the path `<h2>` left, mono **18 px** bold (`break-words`); right, a decorative 8 px status dot (`aria-hidden="true"`, `rounded-full`; published = filled tappa-green, not published = a hollow ring `border-[1.5px] border-ink/45`) + "Live since …" / "Not published. …" in `text-sm text-ink/75` — the words carry the state. The **"public page is behind"** notice sits in its own band **between the header and the form** (as the code orders it, `legal.templ:54-65`). **Form** band `flex flex-col gap-3 pt-[18px] pb-[22px]`: a framed textarea — wrapper `overflow-hidden rounded-[10px] border border-line bg-paper` with a label bar `flex min-h-9 items-center border-b border-line bg-ink/[0.04] px-4` holding the `<label … class="docket-label">Text</label>` (the bar reads TEXT), then the textarea borderless, transparent, `resize-y font-mono text-sm leading-[1.7]` with 18 px vertical padding, `rows="12"`; under it `flex flex-wrap items-center justify-between gap-x-5 gap-y-3`: help on the left, **Publish a new version** on the right, `w-full md:w-auto` (full width on the phone) | `name="body" rows="12" required`, the textarea's `id`, hidden `slug`, one form per document; `op10_test.go:492`'s notice reading (M13-03) |
| **Legal versions** (M13-14) | tablet/desktop (boards' `.vrow`): a **five-column grid** per row, `172px 150px 96px minmax(0,1fr) 64px` on desktop, `164px 136px 92px minmax(0,1fr) 64px` on tablet — `[aria-labelledby="legal-versions"] .op-version { @apply md:grid md:items-center md:gap-x-5 md:min-h-11 md:py-2.5; }` + the columns per breakpoint: time **bold mono** 13 px · path mono · bytes mono `text-ink/70` **right-aligned** · publisher in Space Grotesk 14 px · Live chip (absent on non-live rows, the column stays empty); phone: each row a **stacked card** (time on its own line, then path · bytes · publisher, chip) | `<li class="op-version">` literal (`op10_test.go:308, 631, 656`); the row text stays contiguous ("22 bytes", the publisher, "/legal/…", "Live") |

---

## 7. Screen map

| Boards (desk · tablet · phone) | Route(s) | Handler | Templ component | View type | Card |
|---|---|---|---|---|---|
| shell (every signed-in board) | every signed-in route; sign-out `POST /operator/logout` | `render` (`render.go:72`); `logout` console.go:37 | `bar`, `screen`, `scriptedScreen`, `tenantScreen`, `tenantBanner` (`chrome.templ:33-99`), new `opNav`, `opSubNav` | `TenantName` (`view.go:45`) | M13-01 |
| `OpSignIn` · `T-OpSignIn` · `M-OpSignIn` | `GET`/`POST /operator/login`, `GET`/`POST /operator/login/totp` | `signInPage` signin.go:16, `signIn` :35, `codePage` :62, `code` :75 | `SignIn` signin.templ:18, `Code` :58 | `SignInView` view.go:105-109, `CodeView` :112-115 | M13-04 |
| `OpEnroll` · `T-OpEnroll` · `M-OpEnroll` | `GET /operator/enroll?id=…`, `POST /operator/enroll` | `enrollPage` enroll.go:56, `enroll` :120 | `Enroll` enroll.templ:31 (`scriptedScreen`) | `EnrollView` view.go:129-146 | M13-05 |
| `OpHome` · `T-OpHome` · `M-OpHome` | `GET /operator` | `home` console.go:15 | `Home` home.templ:20 | — | M13-06 |
| `OpProblem` · `T-OpProblem` · `M-OpProblem` | every refusal (constants `render.go:137-452`, list `problemPages()` :456-471) | `render` problem path | `Problem` home.templ:53 | `ProblemView` view.go:558-568 | M13-03 |
| `OpTenants` · `T-OpTenants` · `M-OpTenants` | `GET`/`POST /operator/tenants` | `tenantList` tenants.go:103, `searchTenants` :116 | `Tenants` tenants.templ:23 | `TenantsView` view.go:195-207, `TenantRow` :211-220 | M13-07 |
| `OpTenant` · `T-OpTenant` · `M-OpTenant` | `GET /operator/tenants/{id}`, `POST …/integration`, `POST …/integration/close` | `tenantOverview` tenants.go:176, `enableTenantAPI` tenantapi.go:231, `disableTenantAPI` :271 | `TenantOverview` tenants.templ:119 | `TenantOverviewView` view.go:224-239, `TenantAPIView` :243-257 | M13-08 |
| `OpPlaques` · `T-OpPlaques` · `M-OpPlaques` | `GET /operator/tenants/{id}/plaques` | `tenantPlaques` plaques.go:68 | `TenantPlaques` plaques.templ:16 | `TenantPlaquesView` view.go:262-274, `PlaqueRow` :281-290 | M13-09 |
| `OpBilling` · `T-OpBilling` · `M-OpBilling` | `GET`/`POST /operator/tenants/{id}/billing` | `tenantBilling` billing.go:92, `pageTenantBilling` :111 | `TenantBilling` billing.templ:24 | `TenantBillingView` view.go:394-403, `BillingRow` :412-426 | M13-10 |
| `OpVat` · `T-OpVat` · `M-OpVat` | `GET`/`POST /operator/tenants/{id}/vat` | `tenantVAT` vat.go:155, `recheckVAT` :191 | `TenantVAT` vat.templ:21 | `TenantVATView` view.go:454-480 | M13-11 |
| `OpAudit` · `T-OpAudit` · `M-OpAudit` | `GET`/`POST /operator/audit` | `auditLog` audit.go:83, `filterAudit` :97 | `AuditLog` audit.templ:27 | `AuditLogView` view.go:318-332, `AuditRow` :371-387 | M13-12 |
| `OpHoldings` · `T-OpHoldings` · `M-OpHoldings` | `GET /operator/holdings[?page=N…]`, `POST /operator/holdings`, `POST …/{id}/members`, `POST …/{id}/members/{tenant}/remove` | `holdingsPage` holdings.go:135, `createHolding` :170, `addHoldingMember` :218, `removeHoldingMember` :256 | `Holdings` holdings.templ:24 | `HoldingsView` view.go:508-519, `HoldingView` :525-530, `HoldingMemberView` :536-542 | M13-13 |
| `OpLegal` · `T-OpLegal` · `M-OpLegal` | `GET`/`POST /operator/legal` | `legalPage` legal.go:85, `publishLegal` :160 | `Legal` legal.templ:21 | `LegalView` view.go:150-156, `LegalDoc` :159-177, `LegalVersionRow` :181-190 | M13-14 |

Not boards: the plain-text 404 for unknown `/operator/…` paths, the plain-text 503 when the
surface is unconfigured or the database is down (`surface.go:242-270`), and the plain 500 of a
refused render (`render.go:72-84`).

### Board notes (D1 corrections the port applies)

- The nav's binding label is `aria-label="Operator sections"`, the sub-nav's `aria-label="This tenant"`;
  all fifteen boards that draw a sub-nav carry "This tenant" (relabelled 2026-10-20).
- `OpHome` / `T-OpHome` / `M-OpHome` once drew an icon tile beside each of Home's four links;
  the tiles were removed from the boards and are **not ported** (D1: Home has none today, and a
  tile would add SVGs the signed-in count does not allow).
- The audit boards' "Unrecognised" sample row uses the neutral raw kind `future_kind` (an
  earlier sample named a holding rename, which does not exist).
- `OpEnroll` / `T-OpEnroll` / `M-OpEnroll` draw a **real sample QR** made with the repo's encoder
  (`internal/qrcode`): version 8, 57 modules × 4 px = 228 px, ink `#152219` on paper `#FFFDF4`,
  `class="op-qr"`, `role="img"`, `aria-label` + `<title>` "Scan with your authenticator app" — the
  shape the port renders. Its key, the sample base32 key and the `otpauth://` URI are sample
  data: the port still draws `EnrollView.QR` with `qr.go`'s `qrViewBox` / `qrPixels` / `qrPath`
  (`enroll.templ:39-44`), whose size follows the operator host's length (version 8 at 228 px up
  to an 18-byte host, `qr.go:22-29`).
- The signed-in `OpProblem` states mark **no** nav item current (§2 "Signed-in problem pages").
- The unnamed-tenant banner ("Unnamed tenant" + the id, mono) is now drawn as a state on
  `OpPlaques` and `OpVat` (and their tablet/phone boards) too; the code renders it on all four
  one-tenant screens, and the fixtures that do so are "tenant overview, unnamed", "tenant plaques, unnamed", "tenant billing,
  unnamed" and "tenant VAT, not found" (`op8_test.go:719, 723, 732, 736`).
- `OpProblem` draws `{{pr.title}}` etc.: every title, message and back link is one of the
  constants in `screens.md` §4.
- Board document titles are sample: the real titles are `Sign in`, `Sign-in code`,
  `Set up your account`, `Console`, `Tenants`, `Tenant overview`, `Plaques`, `Billing`,
  `VAT number`, `Audit log`, `Holdings`, `Legal texts`, each `… — Taptime operator` (one-tenant
  screens insert ` — <tenant name>`).

No board element needs a new read or a new route (checked against the inventory, every board,
2026-10-20); there is no "drawn but not built" list for this surface.
