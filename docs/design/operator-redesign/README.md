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

**Citations — written at HEAD `022ba3c` (2026-10-20), re-read on 2026-10-25 at `98546448`.**
The package was written before OP-15 (tenant suspension, ADR 0025; live at `5aaa562`, goose 63),
M15 phase A, the legal-links change (ADR 0033) and the public site (ADR 0034) reached `main`.
Measured with `git diff --quiet 022ba3c 98546448 -- <file>`, the cited files that changed since
are `internal/handler/operator/{render,routes,surface,tenants,audit}.go`,
`web/templates/operatorpages/{view.go,tenants.templ,legal.templ}`, `web/templates/layout/base.templ`,
`web/static/css/input.css` (compared with `27516b7`, where its citations were last read),
`internal/db/operator.go`, `internal/domain/legal/legal.go` and the tests `op8_test.go`,
`op14_test.go`, `op12_db_test.go`, `typepins_test.go` — **every citation into these files points at
`98546448`**, each line re-found by its symbol and read. Every other cited file is byte-identical at
`022ba3c` and `98546448` (`chrome.templ`, `home.templ`, `signin.templ`, `enroll.templ`,
`plaques.templ`, `billing.templ`, `vat.templ`, `audit.templ`, `holdings.templ`, `qr.go`,
`components/notice.templ`, `layout/theme.go`, `enroll.js`, `tailwind.config.js`; the handlers
`console`, `signin`, `enroll`, `plaques`, `billing`, `vat`, `tenantapi`, `holdings` and `legal` `.go`;
the tests `op10`, `op11`, `op12`, `op13`, `op16`, `holdings`, `tenantapi`, `enrollqr`, `op8r4` and
`op14d` `_test.go`), so their citations hold as written at `022ba3c`. The OP-15 surface the boards do
not know is §2 "OP-15 annex" and the screens.md sections it names. Lines move; re-find them by the
symbol named beside each one.

---

## 1. What the operator surface is (read before drawing a pixel)

- **A separate host and a separate shell.** Every route answers only on the operator host; any
  other host gets the router's plain-text 404 (`hostGate`, `internal/handler/operator/routes.go:146-154`).
  The shell is `layout.Operator` / `layout.OperatorWithScript` (`base.templ:601-623`): porcelain
  body, ink text, **`Theme{}`** — no tenant brand, no theme link, no logo.
- **It crosses tenants.** Every data read is an `op_*` SECURITY DEFINER call (CLAUDE.md §4.5
  exception (1), ADR 0021): `op_begin_read` writes and commits an `operator_audit_log` `read` row
  and returns a ticket, `op_read_*` consumes it. The ticket never reaches the handler package and
  has **no screen** (`audit.go:26-27`, `tenants.go:27`, `plaques.go:24-26`, `billing.go:31-34`,
  `legal.go:42-43`). The only visible consequence: *opening* a data screen is itself an audited
  read and costs one unit of the session's read budget (60 / 10 min, `surface.go:508-509`) and
  one of the request budget (100 / 10 min, `surface.go:399-400`).
- **Writes that exist** (nothing else is writable): legal publish; holding create / add member /
  remove member; integration API open / change code / close; VAT re-check (records the VIES
  verdict only); **a tenant's suspension — suspend (a reason of the closed set + a ticked box) and
  lift it (a ticked box)** (OP-15, ADR 0025 §5; `POST /operator/tenants/{id}/suspend`, `…/reinstate`,
  `routes.go:111-112`; screens.md §6). No impersonation, no other tenant edit (ADR 0020 §8).
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
  `aria-current="true"` (the section, not the page — §2 "OP-15 annex" (b)). Content to the right on porcelain, max-width 960 px, **left-aligned** in the column (no `mx-auto` — the boards' `.opmain`, §6 "Content column").
- **Tablet (768–1199, `md:`).** Under the band, a horizontal row (paper, `line` bottom border):
  the same five items as text tabs, 44 px tall; current = ink 3 px bottom marker + bold; wraps if
  needed.
- **Phone (< 768).** The band stays (lockup + sign out), its row **56 px** (64 px from the tablet — §6 "Band"). A **bottom tab bar in paper (not
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
  saffron bottom rule (label "Tenant", name in Space Grotesk bold at **26 / 30 / 34 px** — phone /
  tablet / desktop, §6 "Tenant banner"; **"Unnamed tenant"** + the id,
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

### OP-15 annex (2026-10-25) — what the boards do not know, and three orchestrator decisions

The boards (2026-10-20) were drawn before OP-15 (ADR 0025) reached the code, so none of them
draws a suspension. Under D1 the code's suspension surface is ported as it is, from the inventory
in screens.md: §5 (the tenant list's **Suspended** chip), §6 (the overview's **Panel writes**
card, built from this catalogue's parts — no board), §10 (two audit kinds) and §4 (six problem
pages). The task cards M13-03, M13-07, M13-08 and M13-12 carry its acceptance. Nothing else changes
for a suspended tenant on this surface: its Plaques, Billing and VAT screens and the overview's API
card read no suspension and show none (`plaques.go`, `billing.go`, `vat.go`, `tenantapi.go`,
`plaques.templ`, `billing.templ` and `vat.templ` are byte-identical at `022ba3c` and `98546448`;
the API card, `tenantAPISection`, reads only `TenantAPIView`, which OP-15 did not touch); the banner
names the tenant, not its state.

- **(a) Chip tones — the code's tones stay.** Where a board's chip tone differs from the code's
  `.tally--x` rule, the code wins (D1): the port rounds a chip (`.op-main .tally`, M13-02); it does
  not re-tone one. The only re-tone in this package stays `tally--running` (§6 "Chip / tally",
  decided 2026-10-20). Plaques, measured on `OpPlaques`, `T-OpPlaques` and `M-OpPlaques` (the three
  boards define the same tone set) against `input.css` at `98546448`:

  | Word | Code — kept | Board — not ported |
  |---|---|---|
  | On a wall | `tally--mounted`: `border-tappa-green bg-green-lite` (`input.css:734-739`) | `t-green`: solid tappa-green frame, tappa-green at 10 % ground |
  | On a wall, never encoded · In stock, not encoded | `tally--unencoded`: solid `border-saffron bg-saffron-lite` (`input.css:740-744`) | `t-saffron`: **dashed** saffron frame, saffron at 12 % ground |
  | In stock | `tally--stock`: `border-line bg-line/10` (`input.css:745-750`) | `t-ink`: ink at 50 % frame on paper |
  | Retired · Lost | `tally--withdrawn`: `border-tomato bg-tomato/10` (`input.css:696-699`) | `t-tomato`: the same — no difference |
  | Unrecognised | `tally--unrecognised`: `border-ink bg-ink/10` (`input.css:768-770`) | `t-odd`: **dashed** ink at 50 % frame on paper |

  The same board palette (`t-green`, `t-saffron-solid`, `t-line`, `t-tomato`) is drawn on
  `OpTenant` (API **Open** / **Closed**) and `OpVat` (the four verdicts); the code's tones stay there
  too — M13-02 already keeps every tone rule but `tally--running` (they are shared with the panel),
  and each `Test…Screen_EachChipHasTheRuleTheContrastTestComputes` pins a ground and a frame. The
  suspension's two chips (`tally--tenant-active`, `tally--tenant-suspended`, `input.css:739`, `:699`)
  are on no board and stay as coded.
- **(b) `aria-current`: `"true"` in the main nav, `"page"` in the tenant sub-nav.** The main nav
  (`op-nav`: desktop column, tablet row, phone bar — one DOM) marks its current item
  `aria-current="true"`; the tenant sub-nav (`op-subnav`: Overview · Plaques · Billing · VAT number)
  marks its current tab `aria-current="page"`. **Why:** the main nav names the **section** a page
  belongs to, and the sub-nav names the **page**. A tenant's Plaques page, opened from the Tenants
  list, is in the Tenants section, but it is not the Tenants page — `aria-current="page"` on
  *Tenants* would tell a screen-reader user "current page: Tenants" while they read a tenant's
  plaques, and a one-tenant page would carry two items both claiming to be the page. `"true"` means
  "the current item of this set" (WAI-ARIA), which is what a section marker is. The main nav keeps
  `"true"` on every screen, even where its item's link is the page itself (Console on Home, Tenants
  on the list, Holdings, Audit log, Legal texts), so one attribute has one meaning per nav and the
  shell test checks one value per nav. (The tenant panel's section links carry `"page"` —
  `components/panel.templ:95, 130, 157`; the operator differs on purpose, for the reason above,
  and shares no nav markup with the panel.) Signed-in problem pages carry neither. Bound in: §2 (the shell
  bullets), §4.1 (the extended shell test), §6 "Main nav" / "Tenant sub-nav", screens.md §0 and
  the M13-01 card.
- **(c) The overview's `<h1>` is the page heading, not the tenant's name.** Measured: the overview is
  the one operator data screen without an `<h1>` (`tenants.templ:129` writes only
  `<p class="docket-label">Tenant overview</p>`), while Plaques, Billing and VAT each carry one page
  heading (`plaques.templ:20`, `billing.templ:28`, `vat.templ:25`). M13-08 makes that element
  `<h1 class="docket-label">Tenant overview</h1>` — same word, same class, only the element; no
  test pins the `<p>`. The tenant-name banner is the shell's and stays a `<p>`: as an `<h1>` it would
  give the other three one-tenant screens two, and break its pinned
  `<p class="font-display text-xl font-bold tracking-tight">`. M13-08 adds a test (its name the
  builder's) that every one-tenant render carries exactly one `<h1>`, inside `<main>`, outside the
  banner. Bound in: screens.md §6, the M13-08 card.

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
   `TenantScreen` refuses to render without a name (`view.go:92-99` → plain 500;
   `TestTenantScreen_RefusesToRenderWithoutAName`). Platform-wide screens (Home, Tenants, Audit,
   Holdings, Legal) have no banner; each row names its tenant.
5. **Home makes no read** (`home.templ:7`, `console.go:12-23` — `home` at :15 renders `Home()` and calls no store). A figure on Home would be a
   new audited read and a new `op_*` (a migration, ADR 0021) — not a port.
6. **Read budget.** Each data screen view = one `read` audit row + one budget unit
   (`TestReadBudget_TheReadsOfEveryScreenShareOneBudgetPerSession`; per screen
   `TestTenantPages_AReadCountsTwiceAgainstTheSessionBudget`, `TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget`,
   `TestBillingBudget_EachViewIsOneReadOfTheSessionsSharedBudget`,
   `TestAuditBudget_EachViewIsOneReadOfTheSessionsSharedBudget`,
   `TestHoldingsBudget_AViewIsOneReadAndAWriteIsNone`). A suspension write is one request unit and
   no read; the overview its 303 lands on is the read (`TestSuspensionBudget_AWriteIsOneRequestAndNoRead`).
   **No combined screens** (an overview with inline plaques), no auto-refresh, no widget anywhere.
7. **Package boundary.** `operatorpages` may import only `web/templates/components` and
   `web/templates/layout`, and only `internal/handler/operator` imports it
   (`TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell`). It cannot use the
   panel's `pages.PanelNotice` / `PanelProblem`. `components.Notice` and every `layout.*` are
   shared with the tap / activation screens and byte-pinned (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`)
   — **operator-only components live inside `operatorpages`, unexported**. Unexported matters:
   `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` pins the package's exported
   component constructors to the fourteen screens (`typepins_test.go:1425`). OP-15 added no exported
   constructor: the suspension card's components are unexported parts of `TenantOverview`.
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
    `TestAuditScreen_WearsTheDocketAnatomy` (`op14_test.go:753`, the `op-input`/`op-link` list) is
    extended with the new classes (`op-nav-item`, `op-subnav-item`, `op-pager`).
14. **Red-line touchpoints.** CLAUDE.md §4.5 exception (1) is this surface: **no new `op_*`, no
    widened return column.** Holding membership is written only by `op_*` here (ADR 0027 §2) —
    no rename/delete holding. The VAT number is not editable. The suspension is written only by
    `op_suspend_tenant` / `op_reinstate_tenant` (ADR 0025 §2, migration 00063): its forms gain no
    field — the tenant is the path's, the reason a code of the closed set, no free text
    (`TestSuspension_TheTenantIsThePathsNotTheForms`, `TestTenantOverview_SaysTheSuspensionAndOffersOneForm`).
15. **Tests during a task:** only changed tests with `-run` + `gofmt`, build,
    `bash scripts/redline-check.sh`; the full suite and `make check` once, before the commit
    (CLAUDE.md §10). `make gen` after every `.templ` edit; generated files are committed.

---

## 4. Tests that pin chrome, markup and copy

A port that changes pinned markup updates the test **in the same commit**, keeping every
assertion's intent; nothing is loosened to turn a test green.

### 4.1 The chrome — `TestOperatorScreens_EveryOneWearsTheOperatorChrome` (`op8_test.go:784-821`)

Walks the 49 fixture renders of `screens()` (`op8_test.go:556`), 40 of them signed in, and
requires on **every** render (OP-15 added three signed-in fixtures: "tenants, a suspended row",
"tenant overview, suspended", "tenant overview, an unrecognised reason" — 46 / 37 at `022ba3c`):

- `<header class="op-bar">` **before** `<main` (so nothing may move the band below the page, and
  the nav sits between the two);
- the exact lockup `TAPTIME <span class="text-saffron">OPERATOR</span>`;
- none of `panel-side`, `side-nav`, `bar-link`, `tab-link`, `punchless`, `text-tappa-green">taptime`;
- the sign-out form `action="/operator/logout"` present **exactly** on the 40 signed-in renders;
- a title ending `Taptime operator</title>`.

**How M13-01 extends it (never weakens it):** all five existing checks stay byte-for-byte;
added per render —

- signed-in renders carry **one** `<nav class="op-nav"` (single DOM for all three widths)
  between the band and `<main`, with exactly the five hrefs `/operator`, `/operator/tenants`,
  `/operator/holdings`, `/operator/audit`, `/operator/legal`; signed-out renders carry **none**;
- `aria-current="true"` in the main nav (§2 "OP-15 annex" (b)): exactly one on each of the 39
  signed-in renders other than "problem, signed in" (zero), and it is the right item (Console on
  home; Tenants on the tenant list and the four one-tenant screens; Holdings, Audit log, Legal
  texts on theirs); **no** `aria-current="page"` inside `op-nav` on any render;
- **every one-tenant render (19** — the 18 overview / plaques / billing / VAT fixtures, four of
  them overviews since OP-15, plus the bare "tenant screen" fixture**)** carries the tenant sub-nav
  **inside** the banner (`<section class="op-tenant"` … `<nav class="op-subnav"` … `</section>`,
  before `<main`) with the tenant's four paths and exactly one `aria-current="page"` (Overview /
  Plaques / Billing / VAT number; the bare fixture names which one it passes); no other render
  carries it;
- the panel-marker list grows by the shell classes shipped since (`sheet-`, `panel-well`,
  `panel-stripe`) and the tenant-brand markers (`/brand/` — the theme stylesheet and logo paths,
  `layout/theme.go:41-46` —, `bg-brand`, `--brand`, `<img`); not the bare words "brand" or
  "accent" (the weak-password notice says "a letter with an accent", `enroll.templ:71`)
  — or that part lives in a **new** test next to it (its name is the builder's; do not cite a test
  name before it exists — `TestEveryNamedTestExists`).

### 4.2 Other tests a port must keep green (or update deliberately)

| What | Test(s) | Pins |
|---|---|---|
| Links and actions | `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` (`op8_test.go:883`) | Home's exact `<a href="/operator/holdings" class="op-link">Holdings</a>` and `…/audit" class="op-link">Audit log</a>`; form **counts** on overview (open 1, close 1), holdings, audit (3 when filtered), billing page 3 (2), VAT (1, 0 for an odd number), and since OP-15 the overview's one suspension form — `<form method="post" action="/operator/tenants/<id>/suspend"` once on the active overview and no `/reinstate"`, `…/reinstate"` once on the suspended one and no `/suspend"` (`op8_test.go:921-926`). The counts match **exact opening-tag prefixes, attribute order included** — e.g. `<form method="post" action="/operator/tenants/<id>/integration" class=`, `<form method="post" action="/operator/holdings" class=`, `<form method="post" action="/operator/tenants/<id>/billing">` — so a port keeps `method` before `action`, and keeps or omits the trailing `class=` exactly as today. Every `action`/`href`/`src` must be a mounted same-origin path. The nav adds links, no forms. |
| One form, one button on plaques | `op13_test.go:327-332` (in `TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible`) | the plaques page has exactly **one** `<form` (the sign-out, `<form method="post" action="/operator/logout">`), exactly **one** `<button`, and no `hx-` attribute. So sign-out is one form and one text button at every width; the nav and sub-nav hold neither. |
| SVG count | `TestEnrollQR_OnlyOnTheFirstLoad` (`enrollqr_test.go:202-229`) | 1 SVG on enroll first load, 0 elsewhere → extended in M13-01 |
| QR | `TestEnrollQR_IsTheKeysURIInkOnPaper` | `class="op-qr"`, `role="img"`, aria-label, `#152219`/`#FFFDF4`, order svg → key → uri |
| Script | `TestEnrollScript_IsTheReviewedBody`, `TestEnrollScript_TouchesTheFragmentAndNothingElse` | ids `enroll-token`, `enroll-token-wrap`; the script's body |
| Banner | `TestTenantScreen_RefusesToRenderWithoutAName`, `TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath`, `TestTenantOverview_AnUnnamedTenantIsNamedByItsID` | `<section class="op-tenant" aria-label="Tenant">`, `<bdi>name</bdi>` after it, `<p class="font-display text-xl font-bold tracking-tight">Unnamed tenant</p>` + `<p class="break-all font-mono text-sm">id</p>`, title `<Title> — <name> — Taptime operator` |
| Docket anatomy | `TestAuditScreen_WearsTheDocketAnatomy` (`op14_test.go:711`), `TestBillingScreen_WearsTheDocketAnatomy` (`op12_test.go:1411`) | single `section.docket` with `aria-labelledby`, `<li class="op-row">`, heading markup, `<select id="audit-kind" name="kind" class="op-input">`, `<button type="submit" class="op-link">…</button>` pager (`op14_test.go:742-743`, `op12_test.go:1441`; also `TestBillingScreen_PagesAreFiveOfTwelve`'s form at `op12_test.go:1370-1373`), billing's labelled facts `<span class="text-sm">LABEL <span class="font-mono">…</span></span>` (`op12_test.go:1436-1440`) → the pager class and the billing receipt strip change these **in the same commit** (§6 "POST pager", "Billing receipt strip") |
| Chips | `TestTallyRules_NoToneRuleColoursTheWord`, `TestCompiledCSS_TallyWordIsInkOnEveryChip`, `Test…Screen_EachChipHasTheRuleTheContrastTestComputes` | chip regex `<span class="tally (tally--[a-z-]+)">` — **no extra class on the span**; billing's rule test wants exactly a ground and a frame per tone (`op12_test.go:1532-1583`) → the dashed live tone extends it (§6 "Chip / tally") |
| AA | `TestOperatorChrome_TheColouredTextClearsAA`, `TestTenantScreens_TheTextClearsAA`, `TestSuspensionScreen_TheChipsAndTextClearAA`, `TestLegalScreen_TheTextClearsAA`, `TestAuditScreen_TheTextClearsAA`, `TestBillingScreen_TheChipsAndTextClearAA`, `TestPlaqueScreen_TheChipsAndTextClearAA`, `TestVATScreen_TheChipsAndTextClearAA` | contrasts recomputed from `tailwind.config.js` |
| View fields | `TestPlaqueScreen_NoKeyReachesThePage`, `TestBillingScreen_OffersNoItemisationAnywhere`, `TestAuditScreen_NoCredentialFieldReachesThePage` | the view types' field lists |
| Copy | `TestBillingScreen_EveryStateSaysItsWordAndNoVerdict`, `TestVATScreen_EveryStateSaysItsWordAndSentence`, `TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem`, `TestPlaqueWords_NameEveryShapeAndNothingElse`, `TestAuditWords_NameEveryKindAndNothingElse`, `TestEnroll_TheWeakPasswordNoticeNamesBothLimits`, `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` | sentences and words verbatim |
| Suspension (OP-15) | `TestTenantOverview_SaysTheSuspensionAndOffersOneForm`, `TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice`, `TestTenantList_ASuspendedTenantCarriesTheChip`, `TestSuspensionScreen_EachChipHasTheRuleTheContrastTestComputes`, `TestSuspensionWords_NameEveryReasonAndNothingElse` (`op15_test.go`) | the radios' attribute order, the labels, the confirm box, the warning's parent / id / class / place after the four radios and its closed sentences, the three `input.css` rules (exactly once each), the two chips' markup and rules, the list chip between `<span>plan</span>` and the id span — screens.md §5, §6 |
| Legal notice | `op10_test.go` (`warnNotice` regex) | `<section role="status" class="op-notice op-notice--warn">` — the "public page is behind" notice (M13-03; it was `components.Notice`'s `<section class="border-l-4 border-saffron…">` before) |
| Notice and problem (M13-03) | `TestOperatorNotice_EveryCallerKeepsItsToneAndHeadingInTheOperatorsOwnBox`, `TestProblemPage_IsOneAlertCardWhoseHeadingIsThePagesOnlyH1`, `TestOperatorNotice_TheWordsClearAAOnEveryTone`, `TestOperatorNotice_EachToneRuleKeepsItsStatusColour` (`opnotice_test.go`) | every caller's tone, heading and body sentence, each tone rule's status colour, the role per tone, at most one alert a page, no `components.Notice` frame; every problem constant as one alert card holding the page's one `<h1>`; AA read from the `.op-notice--x` rules |
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
existing `op-*` block at `input.css:1114-1161` at `98546448`; the suspension's three rules sit apart, at `input.css:1711-1713`) and **unexported** templ components in
`web/templates/operatorpages/`. Breakpoints: base = phone, `md:` = tablet, `desk:` = desktop.

| Component | Extends / new | Recipe | Breakpoints · notes |
|---|---|---|---|
| **Band** `op-bar` | existing `.op-bar`, `.op-lockup`, `.op-signout` (`input.css:1114-1120`), `bar(signedIn)` (`chrome.templ:33-46`) | keep `<header class="op-bar">` and the lockup markup byte-for-byte; inner `flex min-h-14 flex-wrap items-center justify-between gap-3 px-4 md:min-h-16 md:px-6` (full width now — it no longer sits in `max-w-3xl`; the boards' row: **56 px** and 16 px sides on the phone, **64 px** and 24 px sides on the tablet and the desktop — orchestrator decision 2026-10-25, the board wins); `.op-signout` gains `rounded-[10px]` | same at every width; one sign-out form. On the phone it stays the **text** button "Sign out" — no icon, no `<svg>` (the plaques page allows exactly one `<button>`, and the SVG count on signed-in renders is exactly the nav's five); `action="/operator/logout"` appears once. |
| **Frame** `op-frame` | new | `flex min-h-[calc(100vh-3.75rem)] flex-col md:min-h-[calc(100vh-4.25rem)] desk:flex-row` (the band's row + its 4 px rule: 56 + 4 px = 3.75rem on the phone, 64 + 4 px = 4.25rem from the tablet, so band + frame fill the viewport; `layout.Operator`'s `<body>` is shared and stays untouched); content column `min-w-0 flex-1` | wraps nav + content column on signed-in screens only. **No `overflow-*` on the frame or any ancestor of the nav** — it would break the phone bar's `sticky`. |
| **Main nav** `op-nav` | new; **one** `<nav class="op-nav" aria-label="Operator sections">`; links only — **no `<form>`, no `<button>`** inside (the plaques page allows exactly one of each, the sign-out) | desk (the boards' `.opside`: 28 px top and bottom, no side padding): `desk:w-[232px] desk:flex-none desk:flex-col desk:border-r desk:border-line desk:bg-paper desk:px-0 desk:py-7`; label `.op-nav-label` `hidden desk:block px-6 pb-2.5 font-mono text-[11px] uppercase tracking-[0.12em] text-ink/70` "PLATFORM" (24 px sides, 10 px above the items); item `.op-nav-item` `flex min-h-11 items-center font-display text-[15px] text-ink hover:bg-ink/5`, on the desktop `desk:pl-[21px] desk:pr-4` (21 px + the 3 px marker = 24 px); current `aria-[current=true]:font-bold` + marker | **tablet** `md:static md:order-first md:mt-0 md:flex md:flex-row md:flex-wrap md:border-t-0 md:border-b md:border-line md:bg-paper md:px-3 md:pb-0` (the boards' `.optabs`: 12 px sides) (the `md:static`, `md:order-first`, `md:mt-0`, `md:border-t-0` and `md:pb-0` undo the phone's sticky bottom bar; desk adds `desk:order-none`), item `md:min-h-11 md:-mb-px md:flex-row md:px-3 md:text-[15px] md:font-normal md:border-b-[3px] md:border-transparent md:aria-[current=true]:border-ink` (the boards' `.optab`: 12 px sides, the 3 px underline over the row's 1 px rule); **desk** item `desk:border-b-0 desk:border-l-[3px] desk:aria-[current=true]:border-ink desk:aria-[current=true]:bg-ink/5`; **phone** `sticky bottom-0 z-10 order-last mt-auto grid grid-cols-5 border-t border-line bg-paper pb-[env(safe-area-inset-bottom)]` — sticky, not `fixed` (polished boards): in the frame's flex column, `order-last` puts it under the content and `mt-auto` pins it to the viewport's bottom edge on a short page; on a long page it sticks while scrolling and comes to rest under the last control, item `relative min-h-[60px] flex-col justify-center gap-[3px] text-[11px] font-medium text-ink/70 aria-[current=true]:text-ink aria-[current=true]:font-bold` with an ink 3 px top marker on current (the boards' `.opbt`: 500 11 px, 3 px gap, the marker over the middle 60 %). Icon `<svg class="size-[22px] md:hidden" aria-hidden="true">`. Phone label vs wider label: `<span class="md:hidden">Audit</span><span class="hidden md:inline">Audit log</span>` (and Legal / Legal texts). The current item carries `aria-current="true"` (§2 "OP-15 annex" (b)) — the `aria-[current=true]:` variants above select it (Tailwind 3.4.17's arbitrary aria variant, the mechanism the sub-nav's `aria-[current=page]:` uses; the independent review of 2026-10-25 compiled it with the repository's 3.4.17 and measured the selector `[aria-current="true"]`). **Not** the panel's sidebar classes. |
| **Content column** | new wrapper `div.op-col` (`min-w-0 flex-1`) + `<main>` replacing `<main class="mx-auto … max-w-3xl … px-4 py-8">` (`chrome.templ:72,83,95`) | `<main class="op-main flex w-full max-w-[960px] flex-col gap-4 px-4 pt-5 pb-16 md:gap-5 md:px-6 md:pt-6 md:pb-12 desk:gap-6 desk:px-10 desk:pt-8 desk:pb-16">` — **left-aligned in the column, no `mx-auto`** (the boards' `.opmain`: 960 px at most, 32 / 40 / 64 px on the desktop, 24 / 24 / 48 px on the tablet, 20 / 16 / 64 px on the phone; orchestrator decision 2026-10-25, the board wins) — the **`op-main`** class is introduced here (M13-01) because the chip rule (M13-02) scopes to it | the sticky bar sits in the flow, so no `pb-24` clearance for a `fixed` bar is needed; the phone's 64 px bottom padding is the board's breathing room above the bar; signed-out screens: `op-main` too, in the signed-out column (§6 "Signed-out card", as built in M13-04) |
| **Tenant banner** `op-tenant` | existing `.op-tenant` (`input.css:1121`), `tenantBanner` (`chrome.templ:54-66`) | keep `<section class="op-tenant" aria-label="Tenant">`, the label `docket-label` "Tenant", the two `<p>` shapes and `<bdi>` exactly; rule stays **1 px** `border-b border-saffron` (as today and as drawn); inner `w-full max-w-[960px] px-4 pt-4 md:px-6 md:pt-5 desk:px-10 desk:pt-6` (left-aligned like `<main>`, no `mx-auto`) with **no bottom padding** — the sub-nav row inside it sits on the rule; the name at the boards' **26 / 30 / 34 px** (phone / tablet / desktop), line height 1.12, tracking -0.03em — set by the parent rule `.op-tenant > div > p:nth-of-type(2)` (the name is the banner's second paragraph in both shapes), never on the pinned `<p class="font-display text-xl font-bold tracking-tight">`; a selector here must not name a utility (Tailwind adds such a rule to every class that applies the utility — measured in M13-01) | in the content column on desktop, full width under the tab row on tablet, under the band on phone **The name breaks anywhere** (M13-08 review): the same parent rule carries `[overflow-wrap:anywhere]`, as the boards' `.optname` does — a tenant chose its name, and a 130-character word without a space widened a 320 px page to 1972 px; pinned by `TestOperatorShell_ThePhoneBarStaysInTheFlowAndTheBannerNameIsItsSecondParagraph` |
| **Tenant sub-nav** `op-subnav` | new; `<nav class="op-subnav" aria-label="This tenant">` **inside** `section.op-tenant`, after the name | `mt-2 -ml-2.5 flex flex-wrap md:-ml-3` (no ground of its own: the banner's saffron-lite); item `.op-subnav-item` `-mb-px inline-flex min-h-11 items-center whitespace-nowrap border-b-[3px] border-transparent px-2.5 font-display text-sm md:px-3 md:text-[15px] text-ink hover:bg-saffron/10 aria-[current=page]:border-saffron aria-[current=page]:font-bold`; the current tab carries `aria-current="page"` (§2 "OP-15 annex" (b)) | phone 14 px and **wraps** (as `M-Op*` draw it); tablet/desk 15 px. Ink on saffron-lite is AA (ink/70 there is already 5.6:1). Paths from the view (`OverviewPath`, `PlaquesPath`, `BillingPath`, `VATPath`) — content, not classes. |
| **Card** `op-card` | existing `.op-card` (`input.css:1122`) | `rounded-[14px] border border-line bg-paper p-5 md:px-6 md:py-[22px]` (M13-02); drop `shadow-sm` — **no shadow** on any operator card (the platform boards' card shadow is removed to match). **Padding alignment (polished boards):** phone card **20 px** all round (`p-5`); tablet/desktop cards 22 px 24 px (`md:py-[22px]`) — 24 px sides, so a card's text and a docket's text start on one vertical line. **In-card spacing `flex flex-col gap-3.5` is not in M13-02's rule** (deliberate, 2026-10-25): `.op-card` is shared by every operator screen and today's card bodies carry their own `mt-*`, so the gap on the rule would double every unported screen's spacing. Each screen card (M13-04, M13-05, M13-06 … M13-14) moves its own screen's cards to `gap-3.5` and drops the bodies' `mt-*` in the same commit — on the markup (`<section class="op-card flex flex-col gap-3.5" …>`; no test pins the `op-card` markup — measured); M13-15 moves the three utilities into `.op-card` once every screen carries them and drops them from the markup. **Exception, kept through the move:** the legal texts' editor card (M13-14, §6.1 "Legal editor"; board `.ed{gap:0;padding:0;overflow:hidden}`) is padding-less with no gap, split in bands — it takes neither `gap-3.5` nor the card padding, and its override survives M13-15's move; on Legal only the intro card takes `gap-3.5`. No other operator board draws a padding-less or gap-less card (boards searched for `gap:0` / `padding:0`, 2026-10-25: only `.ed`, plus links and a list) | label `docket-label` (pinned on the overview `dt`s) or `.op-label` (`font-mono text-[11px] uppercase tracking-[0.12em] text-ink/70`) for new eyebrows; h1: **the screen's own board sets its size** (orchestrator decision (e), 2026-10-26) — `font-display font-bold`, the board's size per width, its line height and the boards' tracking **-0.03em** (`tracking-[-0.03em]`, not `tracking-tight`'s -0.025em); measured on the boards (phone / tablet / desktop, line height): SignIn and Enroll 26 / 30 / 30 px, 1.08 · Problem 24 / 28 / 28, 1.12 (M13-03) · Home 28 at every width, 1.1 (§6.1) · Tenants, Plaques, VAT 28 at every width, 1.1 · Audit, Legal, Billing, Holdings 26 / 28 / 32, 1.1; `text-2xl md:text-[28px] desk:text-[32px]` (24 / 28 / 32 px) only for a screen whose board gives no value (none today) — **one** `<h1>` per page. **Exception:** the overview's `<h1>` stays `docket-label` (`<h1 class="docket-label">Tenant overview</h1>`, the small mono label the `OpTenant` boards draw) — §2 annex (c) |
| **Docket list** | existing `.docket` (`input.css:132-142`, sharp, perforated) + `.op-row` / `.op-version` (`input.css:1158-1161`) | keep `<section class="docket" aria-labelledby="…">` and `<h2 id="…" class="docket-label">` exactly; padding by a **scoped** rule (`.docket` is the panel's too and its `px-4 py-3` stays): `.op-main .docket { @apply px-5 py-4 md:px-6 md:py-[18px]; }` — phone **16 px 20 px**, tablet/desktop **18 px 24 px** — **one scale and one look on every operator screen**: the perforated `.docket` everywhere (the boards' dashed-top shortcut on Tenants / Tenant / Plaques / VAT is being aligned to it) and this single padding scale for every docket and card; no per-screen padding; row `flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-dashed border-line py-2.5 last:border-b-0`; first fact mono bold; facts `text-sm`; ids `break-all font-mono text-xs text-ink/70`. **Face (orchestrator decision (f), 2026-10-26):** in a docket row the **words** are Space Grotesk and the **values** (uid, figure, time, chip) mono; where a **pinned** span joins a word and its value in one text run, that span stays in the docket's mono (why the tenant list's "Since … UTC" is mono, M13-07). Plaques (M13-09) is the first screen built this way: sentence and fact words Space Grotesk, values and chip mono | **sharp** (D2). Desk: facts on one line where they fit; phone: wrap. `<li class="op-row">` / `<li class="op-version">` stay literal (regex-pinned). |
| **Chip / tally** | existing `.tally` + tones (`input.css:685-770`) | `rounded-full` via a **scoped** rule `.op-main .tally { @apply rounded-full px-2.5 py-0.5 font-bold leading-[18px] tracking-[0.08em]; }` (as built in M13-02: 18 px line + 2 px above and below + the 1 px frame = the boards' 24 px; without the line height the chip inherits its parent's — 20 px in a docket, 24 px in a card — and is 26–30 px tall) (`op-main` is put on `<main>` by M13-01's content column), never by adding a class to the span. **`TestTallyRules_NoToneRuleColoursTheWord` reads every rule whose selector names `.tally`** (`readTallyRules`, `op12_test.go:1645-1707`) and allows only the base `.tally` and lists of `.tally--x` whose `@apply` is frames and grounds — so a `.op-main .tally` selector is a finding today. M13-02 extends the reader with exactly one more allowed shape, the scoped `.op-main .tally` rule, whose utilities may be only shape and type ones (`rounded-*`, `px-*`, `py-*`, `min-h-*`, `leading-*`, `font-bold`, `tracking-*` — `leading-*` produces only `line-height`, no colour: measured, M13-02 review; the boards' chip is 24 px tall, 2 px 10 px, bold, tracking .08em) — no `text-*`, `bg-*`, `border-<colour>`, `opacity` — with a mutation control (a colour utility added to the scoped rule is red). (The test was red on the panel's `.tally--pill` from `2afa9db`; the panel card **M12-52** renamed it to `.pill-tally` (`5524505` + `00ca1e6`; `input.css:1307` at `98546448`, live with M12) and the test is green — measured again 2026-10-25 at `98546448`; M12-52's remaining mutation control (a shape utility added to a `.tally--x` rule is a finding) is not in the test — `op12_test.go` is unchanged since OP-12 and holds only the `text-saffron` mutation — and is folded into M13-02's acceptance. M13-02 must never be satisfied by widening what a `.tally--x` rule may apply; its reader extension is only the selector exactly `.op-main .tally`, one `@apply`.) | the span stays `<span class="tally tally--x">` (regex-pinned); word ink, colour in the frame. **One re-toned modifier, no new one:** the polished billing boards draw a live month (`tally--running`, written only by `billing.templ`) as a **dashed saffron outline** — its selector leaves the line group (`input.css:745-750`) for its own one-line rule `.tally--running { @apply border-dashed border-saffron bg-paper; }` (boards' `.t-saffron-o`: a dashed saffron frame on **paper**). The modifier name still matches `tally--[a-z-]+`, so the chip regex is unchanged; M13-02 (where the CSS changes) extends the two billing tests in the same commit — the rule test allows exactly `border-dashed` as a third token for this tone only, the AA test names the running month on its existing ink-on-paper line (16.17:1) instead of the line/10 one. The other billing tones already match the boards (ended-not-closed saffron, frozen green, before sign-up line, unreadable ink). |
| **Fact grid** (`dl`) | existing overview / VAT `dl` | `grid grid-cols-1 gap-4 md:grid-cols-2 desk:grid-cols-4`; big counts keep `<dd class="mt-1 font-mono text-3xl font-bold">` (regex-pinned) | phone 1 col (facts) / 2 col (counts). **The overview's facts follow their boards instead** (M13-08, screen layout — the board wins): phone two columns with the id and the sign-up time across both, tablet two, desktop `320px 200px minmax(0,1fr) minmax(0,1fr)`; its counts four across from the tablet at 30 / 32 / 34 px (screens.md §6 "As built") |
| **Field** `op-input` | existing `.op-input` (`input.css:1123-1126`) | add `rounded-[10px]`; label `op-label`; help `text-xs leading-[1.45] text-ink/70` — **12 px** (orchestrator decision 2026-10-26, M13-07: every board draws the help at 12 px and Enroll's code writes `text-xs`; an earlier draft of this row said 13 px) | keeps `min-h-11` (op14 input.css check). Textarea (Legal) and select (Audit) use it too. |
| **Buttons** | existing `btn btn--primary`, `btn btn--quiet` | `rounded-[10px]`; primary green, quiet paper + `line` border (tomato on hover — Close the API, Remove, **Suspend writes**); **Lift the suspension** is primary (as coded) | one button per form; no confirm step exists (D1) — the suspension's ticked box is a field of its one form (ADR 0025 K15-10), not a step |
| **Link** `op-link` | existing (`input.css:1127`) | unchanged colour (tappa-green on paper 7.73:1); keeps `min-h-11` | "Back to …" links stay as drawn |
| **Inline link hit area** (audit tenant link; also the holdings member link) | new **scoped** rule, no markup change: the audit row's tenant link is `<span class="text-sm">Tenant <a href=… class="op-link">…</a></span>`, pinned byte-for-byte (`op14_test.go:317, 328, 387`) | as the boards' `.rowlink` (`OpAudit`, `OpHoldings`): the link keeps the line's height and gets a **44 px invisible overlay** — `[aria-labelledby="audit-list"] .op-row > span > .op-link { @apply md:relative md:inline-block md:min-h-0; }` + `…::before { content: ''; @apply md:absolute md:inset-x-0 md:top-1/2 md:h-11 md:-translate-y-1/2; }` (transparent, no colour; the boards draw it at every width — on the phone `op-link`'s own `min-h-11` already gives 44 px, so the scoped rule may stay `md:`). **`inline-block`, not `inline`, and the difference from the boards** (M13-02 review, measured): the boards' `.rowlink` is inline, and an inline link that wraps (a tenant name of ~95 characters at 834 px, ~110 at 1440; `tenants.name` has no length limit) is split into two fragments — the overlay, positioned against them, collapsed to an 18 px target (measured 17 px). As one box the link keeps 44 px at one line and at two (124-character name: 44 px at 834 and 1440, no point of a neighbouring row taken). The visible difference: a name too long for the rest of the line starts on its own line under "Tenant" instead of wrapping after it; a name that fits looks as drawn and the row keeps its height. `::before` (the boards and an earlier version of this row say `::after`; `.op-link` uses neither elsewhere) | `.op-link`'s rule itself keeps `min-h-11` (the op12/op14 input.css checks). |
| **Search form** | `Tenants` (`tenants.templ:23-52`, the form at `:28-36`) | `<form method="post" action="/operator/tenants" role="search" class="…">` (the code's attribute order — `method` before `action`, as every operator form); field + button in `flex flex-col gap-2 md:flex-row md:items-end` — **as built (M13-07):** the form's four children stay in the phone's reading order (label, box, help, button; `M-OpTenants`), so the tablet/desktop row (box and button side by side, the help under both — `OpTenants`, `T-OpTenants`) is grid **placement**, not a wrapper: form `flex flex-col gap-2 md:grid md:grid-cols-[minmax(0,1fr)_auto] md:gap-x-2.5`, label `op-label md:col-span-2`, help `text-xs leading-[1.45] text-ink/70 md:col-span-2 md:row-start-3` (12 px, §6 "Field"), button `btn btn--primary md:col-start-2 md:row-start-2`; the box's attributes unchanged (no `aria-describedby`: the code writes none, the boards' is not ported); pinned by `TestTenantsScreen_IsTheSearchCardTheDocketAndThePager` | POST only; the term never becomes a URL |
| **Pager** `op-pager` | **new** class; `auditPageForm` (`audit.templ:195-201`), `billingPageForm` (`billing.templ:129-134`), `holdingPager` (`holdings.templ:186-200` since M13-13; `:165-176` before it) | the polished boards draw Billing's **Newer months / Older months**, Audit's and Holdings' **Previous page / Next page** as **outlined panel-style buttons** (boards' `.pgbtn`): `.op-pager` `inline-flex min-h-11 items-center justify-center whitespace-nowrap rounded-[10px] border border-ink/30 bg-paper px-[22px] font-display text-[15px] font-bold text-ink no-underline hover:border-ink`; the `<nav aria-label="…">` `flex flex-wrap gap-3` (boards' `.pager`) | Audit and Billing stay POST forms (`<form method="post" action=…>` + hidden fields + `<button type="submit" class="op-pager">`); Holdings stays **links** (`<a href="/operator/holdings?page=N" class="op-pager">`). The class change is made **in the tests in the same commit** — `op14_test.go:742-743` (audit), `op12_test.go:1370-1373` and `:1441` (billing) — keeping every structural assertion (element `button`, `type="submit"`, form `method`/`action`, hidden `page`/`kind` fields, label) and adding a control that the old `class="op-link">Previous page</button>` / `Older months</button>` is **absent**, so the new string is not matched alongside a leftover. `.op-pager` joins the 44 px input.css check (`op14_test.go:753`). Ink/30 on paper is a frame, not text (the word is ink, 16.17:1). The tenant list's pager (`tenantPageForm`) is not in the polished list: it keeps `op-link` unless its board is redrawn. |
| **Operator notice** `opNotice` | **new, unexported** `opNotice(tone, heading)` in `operatorpages/notice.templ`; **do not** edit `components.Notice` | ok `rounded-[14px] border border-tappa-green bg-tappa-green/10 px-[18px] py-4`; warn `border-saffron bg-saffron-lite`; alert `border-2 border-tomato bg-paper`; heading stays a `<p class="font-display font-bold">` (M13-03: three test helpers read `>heading</p>`); body `mt-1 text-sm text-ink/85`; `role="status"` (ok, warn) / `role="alert"` (alert) | headings stay the code's (e.g. "Opened.", "Not added — nothing changed."); rules lists as `<ul class="mt-1 list-disc pl-5">` (the hold warning's own list shape). Callers: SignIn, Code, Enroll, Holdings, TenantOverview API, TenantOverview's security-hold warning (OP-15, `tenants.templ:339-343` — its test cuts the warning at the first `</section>`, screens.md §6), VAT, Legal, Problem. **As built (M13-03):** `<section role=… class="op-notice op-notice--ok\|warn\|alert">` — the frames are the `.op-notice` rules of `input.css` (the recipe above, unchanged), the tone is `components.Tone` (the brand's status vocabulary, and the reason `operatorpages` still imports `components`); the root stays a `<section>`, so the hold warning's cut is unchanged; with no heading (the problem page) the body has no top margin. **Orchestrator decision (a), 2026-10-26 — the recipe stays, the boards' left rule is not ported:** the boards draw the notes as a 4 px left rule with `0 10px 10px 0` corners (and, for the alert, a faint 1 px tomato ring); the operator notice keeps this recipe — a full frame with 14 px corners, D2's `rounded-[14px]`, the shape of the panel's `PanelNotice` and its States boards — so the product has one notice shape. Pinned by `TestOperatorNotice_EveryCallerKeepsItsToneAndHeadingInTheOperatorsOwnBox` (each caller's tone, heading and body sentence), `TestOperatorNotice_EachToneRuleKeepsItsStatusColour` (ok tappa-green, warn saffron, alert tomato on paper only — never inverted; every selector naming `op-notice`, in `input.css` and in the compiled sheet, pinned to a known list, so a more specific rule cannot re-colour a tone) and `TestOperatorNotice_TheWordsClearAAOnEveryTone`. |
| **Problem page** | `Problem` (`home.templ:53-65`) | inside the frame: `opNotice(alert)` with the `<h1>` = Title and the message; optional `op-link` Back | signed-out: centred card, no nav; signed-in: nav with no current. No status chip (the view carries no status; D1). **As built (M13-03):** `<div class="op-problem">` (`flex w-full max-w-[640px] flex-col gap-4`, the boards' 640 px and 16 px) around the notice and the way on; the card 20 px 18 px inside on the phone, 24 px 28 px from the tablet (`.op-problem > .op-notice`); the title `<h1 class="op-problem-title">` 24 / 28 px, line height 1.12, tracking -0.03em; the message `mt-2.5 text-[15px] leading-normal` (the boards' 10 px gap and 15 px) written on the markup, not on a card rule (M13-02: no `gap-3.5` move here); the link line 15 px. Signed out it sits in the sign-in's column, which caps it at the boards' 560 px (M13-04, §6 "Signed-out card"); signed in it keeps 640 px. Pinned by `TestProblemPage_IsOneAlertCardWhoseHeadingIsThePagesOnlyH1`. |
| **Signed-out card** | `SignIn`, `Code`, `Enroll`, signed-out `Problem` | today `mx-auto w-full max-w-lg px-4 py-8` (`screen(…, false, …)`; Enroll's `scriptedScreen` the same); card `.op-card` (the problem: its alert card, M13-03) | band without nav or sign out. **The boards differ by screen** (measured 2026-10-26): `OpSignIn`/`T-OpSignIn` **480 px**, `OpProblem`/`T-OpProblem` and `OpEnroll` **560 px**; top / side / bottom 72 / 24 / 96 px on the desktop, 64 / 32 / 80 on the tablet, 24 / 16 / 40 on the phone. SignIn, Code and the signed-out Problem share one column (`screen(false)`), so one width cannot fit both boards: M13-04 sets the shared column's padding and lets each screen carry its own width (the column wide enough for 560 px, the sign-in card `max-w-[480px]`, the problem's `.op-problem` 560 px when signed out) — the M13-04 card. Enroll's column is M13-05's. **As built (M13-04):** `screen(…, false, …)` draws `<main class="op-main mx-auto box-content flex max-w-[560px] flex-col gap-6 px-4 pt-6 pb-10 md:px-8 md:pt-16 md:pb-20 desk:px-6 desk:pt-[72px] desk:pb-24">` — centred, 560 px counting the content alone (`box-content`: the padding is added outside the width, so one `max-w` serves the three paddings); the sign-in and code card `<section class="op-card mx-auto flex w-full max-w-[480px] flex-col gap-3.5">`; the signed-out problem's `.op-problem` keeps its 640 px rule and the column caps it at 560 (no change to `Problem` or its rule; the signed-in problem stays 640). The card's blocks: the `<h1>` and the line under it in one `flex flex-col gap-2` group (the boards' 8 px), the notice (no wrapper, no `mt-*`), the form `flex flex-col gap-[18px]` (the boards' 18 px), fields `gap-1.5`, labels `op-label` (the boards' `.lbl`), the code field `op-input min-h-14 text-[22px] tracking-[0.3em]` (the boards' 56 px, 22 px, .3em), "Start again" `text-sm`. The `<h1>` takes the boards' size, `text-[26px] leading-[1.08] tracking-[-0.03em] md:text-[30px]` (26 px phone, 30 px tablet and desktop) — decision (e), §6 "Card". **Deliberate differences from the boards:** the card's blocks are 14 px apart (`gap-3.5`, §6 "Card"; the boards draw 20 px); field 10 px and button 10 px corners (§6 "Field", "Buttons"; the boards 8 and 12 px); the primary button's type and sides are `.btn--primary`'s, inherited from the panel (16 px text, 16 px sides — `text-base`, `px-4`; the boards' `.btn1` 15 px text, 20 px sides), its 48 px height the boards'; the notice keeps its full frame (decision (a), §6 "Operator notice"). Pinned by `TestSignInScreens_AreOneCardInTheSignedOutColumn`. **As built (M13-05):** `scriptedScreen` draws the very opening tag of `screen(…, false, …)`'s column (one column for the four signed-out screens, 560 px — the `OpEnroll` boards' width), and the enrollment card `<section class="op-card flex w-full flex-col gap-3.5">` fills it. Its blocks: the `<h1>` (the sign-in's size, 26 / 30 px, 1.08, -0.03em) and its line in one `flex flex-col gap-2` group; on a first load step 1 as a `flex flex-col gap-3.5` group (the boards' 14 px) — its `.op-step` heading, then the QR with the sentence and the key beside it from the tablet and under it on the phone (`flex flex-col gap-3 md:flex-row md:items-center md:gap-6`, the boards' 12 and 24 px; the text column `min-w-0 md:flex-1`, so it, not the QR, gives way and the QR keeps its whole pixels per module), then the URI's sentence and the URI (`gap-2`); on a re-render the notices, each a direct child of the card; the form `flex flex-col gap-[18px] border-t border-dashed border-line pt-[22px]` (the boards' dashed rule, 22 px and 18 px), step 2's `.op-step` its first child, labels `op-label`. **Deliberate differences from the boards:** blocks 14 px apart (`gap-3.5`; the boards 24) and the re-render's notices 14 px apart (the boards group them 12 px apart); the key and the URI keep their rules (§6 "QR + key": `.op-key` 18 px and 0.1em where the boards draw 17 px; `.op-uri` square, 8 px vertical padding, where the boards draw 8 px corners and 10 px); the field, button and notice differences are the sign-in's above. Pinned by `TestEnrollScreen_IsOneCardInTheSignedOutColumn`. |
| **QR + key** | existing `.op-qr`, `.op-key`, `.op-uri`, `.op-token-field` (`input.css:1130-1146`) | unchanged; the QR is the only SVG on the enrollment page | order svg → key → uri pinned |
| **Step / card heading** `op-step`, `op-heading` | **new** classes (names the builder's) for the two kinds of in-card heading the polished boards draw where the code writes `docket-label` | `.op-step` `font-display text-base font-bold leading-[1.3] tracking-tight md:text-[17px]` — Enroll's "Step 1 · …" / "Step 2 · …" (16 px phone, 17 px tablet/desktop; line height 1.3, the boards', added in M13-05 — a step wraps on the phone); `.op-heading` `font-display text-[19px] font-bold tracking-tight` — Holdings' "Create a holding" | same words; the element may become an `<h2>` (still one `<h1>` per page); Holdings keeps `id="holding-create"` (the card's `aria-labelledby`). No test pins `docket-label` on these three (re-read at `a8a06f6`, unchanged at `27516b7`). As built (M13-05): Enroll's two are `<h2 class="op-step">`, pinned by `TestEnrollScreen_IsOneCardInTheSignedOutColumn`. |
| **Billing receipt strip** | **new**, replaces the four inline figure spans of `billingRow` (`billing.templ:68-71`) | as the boards' `.figs`: a `<dl class="op-receipt">` of four cells, each `<div><dt class="docket-label">Plan</dt><dd class="font-mono">…</dd></div>` — **PLAN · PEOPLE COUNTED · PRICE PER PERSON · AMOUNT** (10 px mono caps label above the value; the words stay the code's, the caps are CSS); strip `grid grid-cols-2 gap-x-5 gap-y-3 border-y border-dashed border-line py-3 md:grid-cols-[repeat(3,minmax(0,1fr))_minmax(0,1.2fr)]`; values `font-mono text-base`; the Amount cell `text-right` (items at the end), its value `text-xl font-bold` (20 px) | phone 2 × 2. Closed / First charged month / Counted over stay `text-sm` spans. Tests updated in M13-10. |

### 6.1 Screen recipes from the polished boards (2026-10-20)

Look only — every word, form and hidden field stays the code's (`screens.md`); pinned markup
named beside each recipe stays byte-for-byte. Selectors that must not touch a pinned tag are
**scoped** from a parent (`<li class="op-row">` and `<li class="op-version">` stay literal).

| Screen | Recipe | Pinned — keep |
|---|---|---|
| **Home hub** (M13-06) | **one** `op-card` (as the code: `home.templ:22-47`), on the single card scale (§6 "Card"): label `Operator console` mono (`docket-label` as today, or `op-label`); `<h1>` **28 px at every width** (`text-[28px] leading-[1.1]`); the list `mt-[18px] border-t border-line`, each `li` a row with a **dashed** bottom rule (`border-b border-dashed border-line`): on tablet/desktop `md:grid md:grid-cols-[128px_minmax(0,1fr)] md:items-baseline md:gap-x-4 md:py-1` (link left, gloss right), on the phone `flex flex-col items-start pt-0.5 pb-3` (the link — `op-link`, 44 px — above its gloss); links 17 px, gloss `text-sm text-ink/70`; the session sentence is the card's last paragraph, **inside the card**, `mt-[18px] text-sm text-ink/70` (no rule) **As built (M13-06):** `<section class="op-card flex flex-col gap-3.5">` (§6 "Card": the card spaces its blocks, so the list's and the sentence's `mt-[18px]` are not written); the label `op-label` and the `<h1>` (`font-display text-[28px] font-bold leading-[1.1] tracking-[-0.03em]`) in one `flex flex-col gap-1.5` group (the boards' 6 px); the list `border-t border-line`; each item `flex flex-col items-start border-b border-dashed border-line pt-0.5 pb-3 text-[17px] md:grid md:grid-cols-[128px_minmax(0,1fr)] md:items-baseline md:justify-items-start md:gap-x-4 md:py-1` (the 17 px is the item's; `md:justify-items-start` keeps the link its own width, the boards' `justify-self: start`); the gloss `text-sm leading-[1.45] text-ink/70`; the sentence `max-w-[620px] text-sm leading-normal text-ink/70` (the boards' 620 px and 1.5). **Deliberate differences from the boards:** the list and the sentence are 14 px from the block above (`gap-3.5`; the boards 18 px), and the card's bottom padding is the single card scale's 22 px (the boards 24 px); the boards' `aria-labelledby="home-h"` on the card is mock-up markup and not ported (D1). Pinned by `TestHomeScreen_IsOneCardOfFourLinksAndReadsNothing`. | the four `<a href=… class="op-link">` exactly (`op8_test.go:883` pins Holdings and Audit log) — the 17 px size comes from a parent rule, not a class on the `<a>`; the gloss keeps its leading "— " (code content). No icon tile, no docket. |
| **Enroll steps** (M13-05) | "Step 1 · …" / "Step 2 · …" as `.op-step` headings (16 / 17 px, §6) instead of `docket-label`; the QR block under step 1 unchanged | `<svg class="op-qr" role="img" aria-label=…><title>…</title>` and svg → key → uri order (`TestEnrollQR_IsTheKeysURIInkOnPaper`) |
| **Tenants row** (M13-07) | measured on the boards: **desktop** one line, four tracks `minmax(0,1fr) auto 76px auto` (`OpTenants`) — name link · Since … · plan (mono) · id (mono 12 px, `text-ink/70`, right-aligned, no wrap); **tablet** three tracks `minmax(0,1fr) auto 76px`, the id on its own full-width line under them (`T-OpTenants`, `grid-column: 1 / -1`; an earlier draft of this row gave the tablet the desktop's four tracks). So: `[aria-labelledby="tenant-list"] .op-row { @apply md:grid md:grid-cols-[minmax(0,1fr)_auto_76px] md:items-center md:justify-items-start md:gap-x-5 md:py-1.5 desk:grid-cols-[minmax(0,1fr)_auto_76px_auto]; }`, and the id — always the row's **last** child — `[aria-labelledby="tenant-list"] .op-row > :last-child { @apply md:col-span-full desk:col-span-1 desk:col-start-4 desk:row-start-1 desk:justify-self-end; }`. Phone: the name link alone on its first line, 44 px tall (`op-link`'s `min-h-11`), then Since and plan on one wrapping line, the id under them; the phone board's "·" between Since and plan is a CSS gap or `::before`, not a new character (D1). **The Suspended chip (OP-15, on no board):** a row has four children, five for a suspended tenant — `<span class="tally tally--tenant-suspended">Suspended</span>` **between the plan and the id** (`tenants.templ:81-83`). With the id placed explicitly (desktop: line 1, track 4), grid auto-placement puts the chip in the first free cell after the plan — **line 2, under the name** — and a row without it keeps the board's one line; on the tablet the chip takes line 2 under the name and the id line 3; on the phone it flows after the plan. `md:justify-items-start` keeps the chip its own width (a grid item stretches to its track otherwise). No selector here names `.tally` (the tone-rule reader counts any, §6 "Chip / tally"). **As built (M13-07), measured** at 320 / 390 / 834 / 1200 / 1440 (fixture "tenants, a suspended row": a chip row beside a row without one — Since, plan and id start at the same x on both, the chip under the name on tablet and desktop, no horizontal scroll at any width, also with a 138-character name without a space and a 129-character term): the recipe above plus the row's vertical rhythm — phone `gap-y-0.5 pt-1 pb-3` (the board's 2 px and 4 / 12 px), tablet `md:gap-y-0 md:pt-1.5 md:pb-2.5` (6 / 10 px), desktop `desk:pb-1.5`; the name link `> :first-child` takes `basis-full` (alone on its phone line), 15 px, `min-w-0` and `overflow-wrap: anywhere` (a name with no space breaks rather than widening the page); the plan `> :nth-child(3)` `font-mono`; the id `> :last-child` also `basis-full` (its own phone line). The row stays in the docket's mono: "Since" is mono with its time, because `<span>Since … UTC</span>` is pinned whole (`op11_test.go:912`) and the board's sans "Since" beside a mono time would need a span in it; the phone's "·" is the 16 px gap. The four rules are each one `@apply`, and their selectors and utilities are pinned exactly by `TestTenantsRow_TheScopedRulesKeepTheBoardsTracks` (with controls: the id's rule deleted, `md:justify-items-start` or the name's `basis-full` dropped, a track list changed); the markup by `TestTenantsScreen_IsTheSearchCardTheDocketAndThePager`. **Two more differences from the boards, deliberate:** the card's links row sits **14 px** under the block above it (the card's gap) where the boards draw 6 px with `margin-top: -8px` — the card's bodies carry no top margin, negative included (§6 "Card"); and the boards' `aria-labelledby="tenants-h"` on the card (with an `id` on its h1) is not ported — today's code writes neither (D1). | `<li class="op-row">`; `op11_test.go:997`'s `<a href=… class="op-link"><span class="italic">Unnamed tenant</span></a>`; `TestTenantList_ASuspendedTenantCarriesTheChip`: `<span>plan</span>`, the chip, `<span class="break-all text-xs text-ink/70">id</span>` in that order — so the plan's mono and the id's 12 px come from scoped selectors (`> :nth-child(3)`, `> :last-child`), never from a class added to those spans |
| **Suspension card** (M13-08; OP-15, **no board**) | built only from this catalogue's parts — the **Panel writes** `op-card` after the API card: `docket-label` h2, the state chip, the facts `dl` (`md:grid-cols-2`), the form's `<h3>`, the reasons' radio grid, the warning as `opNotice(warn)`, the box, one `btn`. Full recipe and pinned markup: screens.md §6 "Panel writes card". **As built (M13-08):** `op-card flex flex-col gap-3.5`; the chip in a bare `<p>`; the one form under a dashed rule (`border-t border-dashed border-line pt-3.5`), as the API form above it | screens.md §6 |
| **Plaque row** (M13-09) | tablet/desktop (boards' `.plq`): two columns — the uid's **156 px on the tablet** (`T-OpPlaques`) and **168 px on the desktop** (`OpPlaques`), `minmax(0,1fr)` beside it — gap 4 px 24 px, row padding 16 px; the UID in the first column, everything else beside it: wrap chip, sentence and facts in **one** `<div>` after the UID span; `[aria-labelledby="tenant-plaques"] .op-row { @apply gap-x-2.5 py-3.5 md:grid md:grid-cols-[156px_minmax(0,1fr)] md:gap-x-6 md:gap-y-1 md:py-4 desk:grid-cols-[168px_minmax(0,1fr)]; }` (an earlier draft of this row gave the tablet the desktop's 168 px); phone: stacked, UID (15 px bold, `break-all`) and chip on one wrapping head line. **As built (M13-09), measured** at 320 / 390 / 834 / 1200 / 1440 (fixtures: the four `screens()` plaque renders, the boards' seven rows, and a row of long values without a space — a 64-character uid, a 140-character location name, a 122-character stored status, a 64-character successor — under a 130-character tenant name; no horizontal scroll at any width, and with the wrapper's `overflow-wrap` removed the same page is 1213 px wide at 320): the row's rule above (phone: the boards' 10 px between uid and chip, 14 px above and below), the uid `> :first-child { @apply break-all text-[15px]; }`; the row's base `items-baseline` sets the uid's baseline on the chip's. The one wrapper is `<div class="contents [overflow-wrap:anywhere] md:flex md:min-w-0 md:flex-wrap md:items-baseline md:gap-x-[22px] md:gap-y-1.5">`: on the phone it draws no box (`display: contents` — kept, orchestrator 2026-10-26: the element is a plain `div` with no role, Chrome's accessibility tree is the same at 390 and 1440 px, and with the pinned DOM — the row opens with the uid's span, one wrapper after it — it is the only way to put the chip beside the uid on the phone and in the second column from the tablet), so the uid and the chip share the head line and the sentence (`w-full`, pinned) and each fact (`basis-full … md:basis-auto`) take a line; from the tablet it is the second column's wrapping flex — the chip alone on its line, the sentence, then the facts 22 px apart; every value breaks anywhere rather than widening the page. Type as the boards set it (§6 "Docket list", decision (f)), by three rules on the wrapper's children: after the chip (`> div > :nth-child(n+2)`) Space Grotesk; the facts (`:nth-child(n+3)`) at `text-ink/70`, their values (`… > *`) `text-ink`, the values keeping their mono class; the chip stays in the docket's mono. All five rules are one `@apply` each, pinned by `TestPlaqueRow_TheScopedRulesKeepTheBoardsColumns`; the markup by `TestPlaqueScreen_IsTheSummaryCardAndTheDocket`. **Deliberate differences from the boards:** the facts are 14 px, not the boards' 13 (§6 "Docket list": facts `text-sm`); the phone's fact lines sit 4 px apart (the row's `gap-y-1`), the boards' 3; on the phone the head line → sentence and sentence → first fact are 4 px too, the boards' 6 (under `display: contents` the row's one `gap-y-1` spaces every child; a separate gap would need a margin on each); from the tablet the fact lines that wrap sit 6 px apart (`md:gap-y-1.5`), the boards' 4; the rows keep the shared dashed rule **under** each row (`.op-row`), where the boards draw one over each, the first included; the docket heading keeps its pinned 10 px `docket-label`; the boards' `aria-labelledby="plaques-h"` on the card is not ported (D1) | the row still **starts** with `<span class="font-mono font-bold">UID</span>` (`op13_test.go:285`); still no `<form`/`<button` beyond sign-out (`op13_test.go:327-332`) |
| **VAT "On file"** (M13-11) | the two facts side by side on tablet/desktop: `dl` (`vat.templ:41`) `flex flex-col gap-3 md:grid md:grid-cols-[max-content_minmax(0,1fr)] md:gap-x-14 md:gap-y-3`; stacked on the phone; the sentence and the time line under them, full width. **As built (M13-11), measured** at 320 / 390 / 834 / 1200 / 1440 (fixtures: the six `screens()` VAT renders, the board's confirmed state, and a 64-character number without a space under a 130-character tenant name without one, with a notice; no horizontal scroll at any width): `<dl class="mt-3 flex flex-col gap-3 md:grid md:grid-cols-[minmax(0,max-content)_minmax(max-content,1fr)] md:gap-x-14">` — the board's picture (the number's column as wide as the number, the chip 56 px after it: 108 px + 56 at 834 and 1200) and room for a long number, which breaks (`break-all`, pinned) beside the whole chip. **Deliberate difference from this row's first recipe:** with `max-content` as the number's track, the long number takes its whole width and the page scrolls (856 px at 834, the chip pushed out — measured); with `md:grid-cols-2` (the card's first wording) the chip starts at the row's middle (445 px at 834, 740 at 1200), not beside the number as the boards draw it. The verdict's `dd` takes the board's 6 px (`mt-1.5`); `gap-3` is both gaps, so no `md:gap-y-3`. The docket keeps its own rhythm (`mt-3` / `mt-3` / `mt-2`, the boards' 12 / 12 / 8 px); the sentence and the time line are `font-display` (decision (f)), the time line `text-ink/70` with its time in ink by one scoped rule, `[aria-labelledby="tenant-vat"] > p > span { @apply text-ink; }` (the word is a bare text run and the time's span is pinned, `op16_test.go`). The intro card is `op-card flex flex-col gap-3.5`: `op-label`, the h1 at 28 px at every width (1.1, -0.03em), the intro `text-sm`, the links stacked on the phone and in a row from the tablet. The asking card `op-card flex flex-col gap-3.5` keeps its `docket-label` h2; the sentence and the form share one wrapper `<div class="flex flex-col gap-3.5 md:flex-row md:items-center md:gap-6">` (the boards' row: the sentence at most 560 px, the button at the row's end), the form `class="flex flex-col md:ml-auto md:block md:shrink-0"` (on the phone the button takes the card's width, as `M-OpVat` draws it); the form's opening tag keeps `method` before `action` and no field. Pinned by `TestVATScreen_IsTheIntroCardTheRecordAndTheAsking` (the whole column, per state) and `TestVATRecord_TheScopedRuleKeepsTheTimeInInk`. **Other deliberate differences:** the docket heading and the asking card's heading keep the 10 px `docket-label` (boards `.lbl` 11 px); the two terms of the list, "VAT number" and "VIES answer", keep `<dt class="docket-label">` — 10 px, 0.1em — where the boards draw `.lbl`, 11 px, 0.12em; the docket's text 14 px / 20 px (boards 14 / 1.5); the boards' `aria-labelledby="vat-h"` on the intro card is not ported (D1); the notice keeps its full frame (decision (a)) | `<dd class="mt-1 break-all font-mono text-lg font-bold">` (`op16_test.go:1409`) |
| **Holdings** (M13-13) | **As built (M13-13).** "Create a holding" is `<h2 id="holding-create" class="op-heading">` (19 px; the id kept for the card's `aria-labelledby`); the Name field and Create share a line from the tablet and the help sits under them, the document keeping the code's order (label · field · help · button — the help takes `order-last`). **The holding's head**, no wrapper: the `<li>` is a wrapping flex whose name (`:first-child`, 21 px) grows and whose creation time (`:nth-child(2)`) sits at the line's end on the name's baseline from the tablet; the id (`:nth-child(3)`) is pulled 2 px under the name; on the phone the name and the id take `order: -1`, so the three stack name · id · time. **The member row** (boards' `.mrow`) keeps the code's DOM order — name, chip, Timezone, API code, Joined, [Left], id, [Remove] — with three order-preserving `<div>`s (name + chip · Timezone + API code · Joined + Left), then the id span and the Remove form as they were (the form still class-less, `op8_test.go:1327`); the rules place each child by grid line numbers: desktop four tracks `minmax(0,1fr) 172px 212px 96px`, the id in row 2 under the name, the facts and Remove across both rows, rows `auto 1fr` so the name's row is the name's height and the id stays 3 px under it however tall the facts beside it (M13-13 review, measured: with rows sharing the extra height the id slid 3.5–22 px down beside a long zone name; now 3 px at 320 / 390 / 834 / 1200 / 1440 on every fixture, zone names Europe/Malta, Europe/Istanbul, America/New_York, America/Argentina/ComodRivadavia); tablet three tracks `minmax(0,1fr) minmax(0,1.15fr) 92px`, the name and the id across two, the facts side by side in row 3, Remove centred across all three; phone one column, name · id · facts · Remove ≥ 120 px. Every rule is scoped from the docket's heading id (`[aria-labelledby="holding-list"]`; no class added to the `ol`): eighteen `@apply` rules plus the name link's `::before` 44 px overlay from the tablet (§6 "Inline link hit area"; the phone keeps the link's own 44 px). Every name and id breaks anywhere (`[overflow-wrap:anywhere]` on the holding's `<li>`). The docket row is §6 "Docket list"'s (orchestrator decision, M13-13 review): the facts `text-sm` (14 px) — words Space Grotesk at ink 70 %, values mono in ink, a time never breaking (whole, under its word, where the 212 px track cannot hold "Joined" and it at 14 px) — and each membership under the shared dashed 1 px rule, the last without one; the list opens with a dashed 1 px rule; holdings are separated by the boards' 2 px dashed rule; the add form is the boards' dashed box (`bg-ink/[.03]`, the audit filter's ground; the board's .025). Measured at 320 / 390 / 834 / 1200 / 1440 (the six `screens()` holdings renders, a page of long values — a holding name of 120 characters and a business name of 130 with no space, the longest IANA zone, a 16-character API code, an unnamed holding and member, both page cuts, the four rules — and eight memberships across four zones): no render scrolls sideways, every link, button and field of the column is ≥ 44 px. **Deliberate differences from the boards:** the facts are 14 px, the boards' `.fact` 13 px, and the member rows' rule is dashed, the boards' solid (both §6 "Docket list"); the help texts stay 12 px (orchestrator decision, M13-07; the boards' `.small` is 13 px); Create and Add keep `btn--primary`'s 48 px (the boards' `.btn1` is 44 px); the chips keep the code's tones (decision (a): Ended is `tally--deactivated`'s line ground). Pinned by `TestHoldingsScreen_IsTheCreateCardAndTheDocket` (the whole column, tags and words, every membership — so the DOM order too), `TestHoldingsDocket_TheScopedRulesKeepTheBoardsGrid` (the rules), `TestOperatorHeadings_TheRulesKeepTheBoardsSizes` (`.op-heading`'s 19 px and `.op-step`'s 16 / 17 px, in `input.css` and in §6's recipe) and `TestHoldingsScreen_TheTextClearsAA` | `<span class="tally tally--active">Live</span>` / `…--deactivated">Ended</span>` (`holdings_test.go:689-690, 987`); form-count prefixes (`op8_test.go:1323-1327`); `href="/operator/holdings?page=2"` (`op8_test.go:1329-1330`, `holdings_test.go:741, 757`) |
| **Billing row** (M13-10) | month + chip on the first line; sentence; the **receipt strip** (§6) for the four figures; then Closed / First charged month / Counted over and the free-window / floor sentences as today. **As built (M13-10), measured** at 320 / 390 / 834 / 1200 / 1440 (fixtures: the four `screens()` billing renders, the board's page-1 rows, and a page of long values — an 87-character plan, a five-digit count, six-digit amounts, the zone `America/Argentina/ComodRivadavia`, a 137-character tenant name; no horizontal scroll at any width, and with the row's `overflow-wrap` removed the same page is 613 px wide at 320 and 390): the row stays flat (it must open with the month's span) — month, chip, the sentence (`w-full`), `<dl class="op-receipt">`, the dated facts in **one** wrapper `<p class="flex w-full flex-col gap-0.5 font-display text-ink/70 md:flex-row md:flex-wrap md:gap-x-[22px] md:gap-y-1">` (one per line on the phone, 22 px apart from the tablet; the spans themselves unchanged), then the free-window and floor sentences; a month with no figure has no strip at all (§4.6). Seven rules scoped from the docket's heading id, one `@apply` each: the heading `border-b-2 border-dashed border-line pb-3` and its page numbers `font-bold text-ink` (the boards' `.dh`); the row `gap-x-3.5 gap-y-3 py-[18px] [overflow-wrap:anywhere] last:pb-1.5 max-md:justify-between md:py-5` (the boards' 12 px between blocks, 14 px month → chip, 18 / 20 px padding, the last row 6 px; on the phone the chip at the head line's end as `M-OpBilling` draws it); the month `text-base` (16 px); the sentences `> span:nth-child(n+3)` Space Grotesk; the strip `> dl` `w-full`; the dated values `> p > span > span` ink. The oldest-page note is the boards' `.dfoot` (`mt-1.5 border-t-2 border-dashed border-line pt-3 font-display text-[13px] text-ink/70`). The pager's `<nav>` is `flex flex-wrap gap-2.5 md:gap-3 max-md:*:grid max-md:*:flex-1` — on the phone each form grows from nothing and its button fills it: where the row has room for both they stand side by side, half each (390 px: 174 + 174), and at 320 px they stack, because a button does not break its word (about 149 px, more than half the row's 139) — as the boards' `.pager > *{flex:1 1 0}` with `.pgbtn{width:100%; white-space:nowrap}` does; the forms' and buttons' pinned markup unchanged. The card: `op-label`, the h1 26 / 28 / 32 px (decision (e)), the intro at the board's 15 px / 1.55, the note at the board's 14 px (`text-sm text-ink/70` — the billing board overrides its `.small` to 14 px), the links' row as Plaques'. Pinned by `TestBillingScreen_IsTheIntroCardAndTheDocket` (the whole column, tags and words) and `TestBillingRow_TheScopedRulesKeepTheBoardsReceipt` (the seven rules and the strip's four). **Deliberate differences from the boards:** the free-window and floor sentences stay plain sentences (this row's "as today"), not the boards' `.baside` boxes; the dated facts are 14 px with regular-weight values, as on Plaques (the boards' 13 px, bold values); the sentence is ink, not ink at 80 %; rows keep the shared dashed rule under each row, not the boards' solid one; the card's gap is 14 px (§6 "Card"), the boards' 12; the strip's labels keep `docket-label`'s tracking (0.1em, boards 0.14em) and the values the 1.5 line height without the boards' 3 px gap; the strip's labels keep the docket's 20 px line height, so a cell is 44 px tall (boards `.figl` 10 px / 1.3, about 37 px); the docket heading stays the pinned 10 px `docket-label` (boards `.lbl.dh` 11 px, 0.14em); when the chip wraps under the month the two are 12 px apart (the row's `gap-y-3`) and the month's line is 24 px (`text-base`), the boards' 8 px and 20.8 px. **The Amount is right-aligned at every width** (orchestrator decision, 2026-10-26): the phone board (`M-OpBilling`) sets the Amount cell left on the phone (`.fig--amt{text-align:left}`), but the strip is a component and its form is this catalogue's (§6 "Billing receipt strip", built in M13-02), so the phone keeps the recipe's right alignment. At 320 px a ten-character amount (`€123456.78`, 20 px bold) breaks inside its 114 px cell rather than widen the page; it fits from 390 | `<li class="op-row">`, `^\s*<span class="font-mono font-bold">Month` (`op12_db_test.go:168`) |
| **Audit row** (M13-12) | operator and account names in **Space Grotesk** (the code writes them in `<bdi>` with no mono — `audit.templ:92, 98`); mono only where the code prints it: time, page, session, ids, document slug, bytes; the inline tenant link gets the 44 px overlay (§6 "Inline link hit area") on tablet/desktop. **As built (M13-12), measured** at 320 / 390 / 834 / 1200 / 1440 (fixtures: the five `screens()` audit renders, the board's fourteen rows, and a page of long values — a tenant, an operator and an account name of 130 characters with no space, an unnamed tenant, an id no tenant has, the two OP-15 rows, a 63-character raw kind; no horizontal scroll in any of the 35 renders): the row's markup is **unchanged** (no wrapper: the M13-02 overlay's selector `.op-row > span > .op-link` names the fact span as the row's child). Nine rules scoped from the docket's heading id, one `@apply` each, plus M13-02's overlay: the heading `border-b-2 border-dashed border-line pb-3` and its page number `font-bold text-ink` (the boards' `.dh`); the row `flex-col items-start gap-y-1 py-3.5 [overflow-wrap:anywhere] last:pb-1.5 md:flex-row md:items-baseline md:gap-x-[18px] md:gap-y-0.5 md:py-4 md:pl-[206px] desk:pl-[220px]` — on the phone one column, each child a line, 4 px apart and 6 px under the kind (the boards' `.abody` gap; the kind's 2 px margin on the row's 4 px gap); from the tablet the boards' two columns **without a grid**: the row is indented by the first column (182 / 196 px + 24 px) and the time (`> :first-child`: `whitespace-nowrap text-[13px] md:-ml-[206px] md:w-[188px] desk:-ml-[220px] desk:w-[202px]`) is pulled back into the indent, its outer size minus the 18 px gap, so the kind (`> :nth-child(2)`: `mb-0.5 text-[15px] leading-[1.35] md:mb-1 md:basis-full`) fills the first line beside it and every fact wraps to the indent, 18 px apart; time and kind share the baseline; the facts (`> :nth-child(n+3)`) `font-display text-ink/70`, their values (`> :nth-child(n+3) > :not(a)`) `text-ink`, the link keeping its colour; on the phone the tenant's fact (`> span:has(> .op-link)`) `max-md:flex max-md:flex-wrap max-md:items-center max-md:gap-x-1.5`, its word centred on the 44 px link as `M-OpAudit` draws it (on a line box it sat above the link's). The card: `op-label`, the h1 26 / 28 / 32 px (decision (e)), the intro 15 px / 1.55, the filter as the boards' framed box (`<form … class="flex flex-col gap-2 rounded-xl border border-line bg-ink/[.03] p-3.5 md:px-[18px] md:py-4">`; the Kind label `op-label`; the pinned `<select … class="op-input">` in a box of its own, `min-w-0 max-w-[440px] flex-[1_1_240px]`, which gives it Space Grotesk 15 px and the button's height through child variants; Show `max-md:basis-full`), the way back. The empty sentences 15 px / 1.5 Space Grotesk (`leading-normal`, the boards' `.dempty`; the docket's own line height made them 20 px), the last-page note the boards' `.dfoot`, the pager as Billing's: on the phone each button fills its form and never breaks its word (the board's `.pager > *{flex:1 1 0}`) — half the row each at 360 and 390 px; at 320 px still one row but 148 / 130 px, Previous page's word being wider than half. Pinned by `TestAuditScreen_IsTheFilterCardAndTheDocket` and `TestAuditRow_TheScopedRulesKeepTheBoardsColumns`. **Deliberate differences from the boards:** the facts are 14 px (§6 "Docket list"), the boards' 13.5; names at regular weight, the boards' 500; rows keep the shared dashed rule under each row, not the boards' solid one; the select keeps the browser's own arrow (the boards draw one with a background gradient); on the phone the select is 44 px and Show 48 px (each on its own line), from the tablet both 48; the card's gap 14 px (§6 "Card"), the boards' 12 | `op14_test.go:317, 328, 387` tenant-link strings |
| **Legal editor** (M13-14) | **one full-width `op-card` per document** (as the code: `legal.templ:47-77`), the card itself without padding, split in bands (boards' `.ed*`) whose side inset is the single card scale — **24 / 24 / 20 px** desktop / tablet / phone (the boards' `--edpx`, aligned 2026-10-20 from 32 / 26 / 18 so the editor's header and form line up with every other operator card and docket; §6 "Card"; no per-screen padding). **Header** `flex flex-wrap items-center justify-between gap-x-5 gap-y-1.5 border-b border-line pt-5 pb-4`: the path `<h2>` left, mono **18 px** bold (`break-words`); right, a decorative 8 px status dot (`aria-hidden="true"`, `rounded-full`; published = filled tappa-green, not published = a hollow ring `border-[1.5px] border-ink/45`) + "Live since …" / "Not published. …" in `text-sm text-ink/75` — the words carry the state. The **"public page is behind"** notice sits in its own band **between the header and the form** (as the code orders it, `legal.templ:54-65`). **Form** band `flex flex-col gap-3 pt-[18px] pb-[22px]`: a framed textarea — wrapper `overflow-hidden rounded-[10px] border border-line bg-paper` with a label bar `flex min-h-9 items-center border-b border-line bg-ink/[0.04] px-4` holding the `<label … class="docket-label">Text</label>` (the bar reads TEXT), then the textarea borderless, transparent, `resize-y font-mono text-sm leading-[1.7]` with 18 px vertical padding, `rows="12"`; under it `flex flex-wrap items-center justify-between gap-x-5 gap-y-3`: help on the left, **Publish a new version** on the right, `w-full md:w-auto` (full width on the phone) | `name="body" rows="12" required`, the textarea's `id`, hidden `slug`, one form per document; `op10_test.go:492`'s notice reading (M13-03) |
| **Legal versions** (M13-14) | tablet/desktop (boards' `.vrow`): a **five-column grid** per row, `172px 150px 96px minmax(0,1fr) 64px` on desktop, `164px 136px 92px minmax(0,1fr) 64px` on tablet — `[aria-labelledby="legal-versions"] .op-version { @apply md:grid md:items-center md:gap-x-5 md:min-h-11 md:py-2.5; }` + the columns per breakpoint: time **bold mono** 13 px · path mono · bytes mono `text-ink/70` **right-aligned** · publisher in Space Grotesk 14 px · Live chip (absent on non-live rows, the column stays empty); phone: each row a **stacked card** (time on its own line, then path · bytes · publisher, chip) | `<li class="op-version">` literal (`op10_test.go:308, 631, 656`); the row text stays contiguous ("22 bytes", the publisher, "/legal/…", "Live") |

---

## 7. Screen map

| Boards (desk · tablet · phone) | Route(s) | Handler | Templ component | View type | Card |
|---|---|---|---|---|---|
| shell (every signed-in board) | every signed-in route; sign-out `POST /operator/logout` | `render` (`render.go:72`); `logout` console.go:37 | `bar`, `screen`, `scriptedScreen`, `tenantScreen`, `tenantBanner` (`chrome.templ:33-99`), new `opNav`, `opSubNav` | `TenantName` (`view.go:46`) | M13-01 |
| `OpSignIn` · `T-OpSignIn` · `M-OpSignIn` | `GET`/`POST /operator/login`, `GET`/`POST /operator/login/totp` | `signInPage` signin.go:16, `signIn` :35, `codePage` :62, `code` :75 | `SignIn` signin.templ:18, `Code` :58 | `SignInView` view.go:106-110, `CodeView` :113-116 | M13-04 |
| `OpEnroll` · `T-OpEnroll` · `M-OpEnroll` | `GET /operator/enroll?id=…`, `POST /operator/enroll` | `enrollPage` enroll.go:56, `enroll` :120 | `Enroll` enroll.templ:31 (`scriptedScreen`) | `EnrollView` view.go:130-147 | M13-05 |
| `OpHome` · `T-OpHome` · `M-OpHome` | `GET /operator` | `home` console.go:15 | `Home` home.templ:20 | — | M13-06 |
| `OpProblem` · `T-OpProblem` · `M-OpProblem` | every refusal (constants `render.go:137-503`, list `problemPages()` :507-524) | `render` problem path | `Problem` home.templ:53 | `ProblemView` view.go:592-602 | M13-03 |
| `OpTenants` · `T-OpTenants` · `M-OpTenants` | `GET`/`POST /operator/tenants` | `tenantList` tenants.go:103, `searchTenants` :116 | `Tenants` tenants.templ:23 | `TenantsView` view.go:196-208, `TenantRow` :212-224 | M13-07 |
| `OpTenant` · `T-OpTenant` · `M-OpTenant` | `GET /operator/tenants/{id}`, `POST …/integration`, `POST …/integration/close`, `POST …/suspend`, `POST …/reinstate` | `tenantOverview` tenants.go:176, `enableTenantAPI` tenantapi.go:231, `disableTenantAPI` :271, `suspendTenant` suspension.go:158, `reinstateTenant` :193 | `TenantOverview` tenants.templ:126 (`tenantAPISection` :182, `tenantSuspension` :254, `suspendForm` :300, `holdWarning` :357, `reinstateForm` :387) | `TenantOverviewView` view.go:228-245, `TenantAPIView` :249-263, `TenantSuspensionView` :269-282, `SuspensionReasonOption` :287-290 | M13-08 |
| `OpPlaques` · `T-OpPlaques` · `M-OpPlaques` | `GET /operator/tenants/{id}/plaques` | `tenantPlaques` plaques.go:68 | `TenantPlaques` plaques.templ:16 | `TenantPlaquesView` view.go:295-307, `PlaqueRow` :314-323 | M13-09 |
| `OpBilling` · `T-OpBilling` · `M-OpBilling` | `GET`/`POST /operator/tenants/{id}/billing` | `tenantBilling` billing.go:92, `pageTenantBilling` :111 | `TenantBilling` billing.templ:24 | `TenantBillingView` view.go:428-437, `BillingRow` :446-460 | M13-10 |
| `OpVat` · `T-OpVat` · `M-OpVat` | `GET`/`POST /operator/tenants/{id}/vat` | `tenantVAT` vat.go:155, `recheckVAT` :191 | `TenantVAT` vat.templ:21 | `TenantVATView` view.go:488-514 | M13-11 |
| `OpAudit` · `T-OpAudit` · `M-OpAudit` | `GET`/`POST /operator/audit` | `auditLog` audit.go:83, `filterAudit` :97 | `AuditLog` audit.templ:27 | `AuditLogView` view.go:351-365, `AuditRow` :405-421 | M13-12 |
| `OpHoldings` · `T-OpHoldings` · `M-OpHoldings` | `GET /operator/holdings[?page=N…]`, `POST /operator/holdings`, `POST …/{id}/members`, `POST …/{id}/members/{tenant}/remove` | `holdingsPage` holdings.go:135, `createHolding` :170, `addHoldingMember` :218, `removeHoldingMember` :256 | `Holdings` holdings.templ:39 (`:24` before M13-13) | `HoldingsView` view.go:542-553, `HoldingView` :559-564, `HoldingMemberView` :570-576 | M13-13 |
| `OpLegal` · `T-OpLegal` · `M-OpLegal` | `GET`/`POST /operator/legal` | `legalPage` legal.go:85, `publishLegal` :160 | `Legal` legal.templ:21 | `LegalView` view.go:151-157, `LegalDoc` :160-178, `LegalVersionRow` :182-191 | M13-14 |

Not boards: the plain-text 404 for unknown `/operator/…` paths, the plain-text 503 when the
surface is unconfigured or the database is down (`surface.go:253-281`), and the plain 500 of a
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
- **No board draws OP-15** (the boards predate it): the list's Suspended chip, the overview's Panel
  writes card, the two audit kinds and the six suspension problem pages are ported from screens.md
  §5, §6, §10 and §4 (§2 "OP-15 annex"). The boards write `aria-current="page"` on the main nav's
  current item as well (e.g. `OpHome`, `OpTenant`); that is mock-up markup — the port follows
  decision (b).
- The unnamed-tenant banner ("Unnamed tenant" + the id, mono) is now drawn as a state on
  `OpPlaques` and `OpVat` (and their tablet/phone boards) too; the code renders it on all four
  one-tenant screens, and the fixtures that do so are "tenant overview, unnamed", "tenant plaques, unnamed", "tenant billing,
  unnamed" and "tenant VAT, not found" (`op8_test.go:741, 747, 756, 760`).
- `OpProblem` draws `{{pr.title}}` etc.: every title, message and back link is one of the
  constants in `screens.md` §4.
- Board document titles are sample: the real titles are `Sign in`, `Sign-in code`,
  `Set up your account`, `Console`, `Tenants`, `Tenant overview`, `Plaques`, `Billing`,
  `VAT number`, `Audit log`, `Holdings`, `Legal texts`, each `… — Taptime operator` (one-tenant
  screens insert ` — <tenant name>`).

No board element needs a new read or a new route (checked against the inventory, every board,
2026-10-20); there is no "drawn but not built" list for this surface.
