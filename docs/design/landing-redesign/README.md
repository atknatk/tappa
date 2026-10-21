# Public site redesign — handoff for the port

This folder is the handoff package for porting the **public site** — the landing page `/`, five
new pages, the share cards, the public 404 and the shared public chrome that Signup, Legal and
the API guide move onto — from the approved boards (2026-10-21) to templ + `.lp` CSS. A developer
agent should be able to port every public screen from this README and the LP task cards without
discovering surprises in the code.

- **Boards:** [`boards/`](boards/) — one `.dc.html` file per board and width: `<Name>.dc.html`
  (desktop, 1440, fluid), `T-<Name>.dc.html` (tablet, 834), `M-<Name>.dc.html` (phone, 390).
  23 files: Landing ×3, LandingDemo (desktop + phone), Product ×3, Pricing ×3, Security ×3,
  ForRestaurants ×3, ForProduction ×3, ShareCards (one board, 2640 wide), States-Public (desktop +
  phone). [`boards/canvas.json`](boards/canvas.json) is the canvas layout reduced to these 23
  boards and the one note that titles them ("Public site — landing & pages"); the panel and
  operator entries of the source canvas are not carried, and its title is "Taptime Public Site
  Redesign" (the source canvas's title named the backoffice). The board files are byte-for-byte
  the exported files — not edited.
- **Task cards:** [`docs/plan/lp-landing-redesign.md`](../../plan/lp-landing-redesign.md)
  (LP-01 … LP-15, ordered, each independently shippable on the branch).
- **Sibling packages** (read for method, not for rules — the public site has its own):
  [`panel-redesign`](../panel-redesign/README.md) §1 (how to read a board),
  [`operator-redesign`](../operator-redesign/README.md) (the same kind of package for M13).
- **Why this order:** the user decided the public site goes **before** the operator port (M13),
  2026-10-21.

All `file:line` citations here refer to **`origin/main` `0e7c158`** (the branch `lp-redesign` was
cut there). Lines move; re-find them by the symbol named beside each one. Citations marked
*(gap report)* were measured by the orchestrator's gap analysis on the same commit and were **not
re-read** for this package; the builder re-reads each before relying on it.

---

## 1. How to read these boards

Panel README §1 applies unchanged (helmet + Google Fonts link, inline `style=` on almost every
element, `{{binding}}` placeholders, `<sc-for>`/`<sc-if>`, a trailing `<script type="text/x-dc">`
with sample data, `data-props` design-time tweaks). Public-site specifics:

- **The copy lives in two places.** Landing, LandingDemo, Product and Pricing share one data
  object in their trailing script (`NEW.prod`, `NEW.price`, `NEW.people`, `NEW.venues`,
  `NEW.plaques`, `NEW.guards`, `NEW.integ`, `NEW.team`, `NEW.shift`, `NEW.sample`, `NEW.baseRows`
  — the same object is pasted into all four files, so a Product sentence also appears in
  `Landing.dc.html`'s script without being rendered there). Security, ForRestaurants,
  ForProduction, ShareCards and States-Public carry their copy inline in the markup.
- **`{{heroLede}}`, `{{priceHeading}}` …** on the Landing board are the existing
  `pages.Landing*` values (`web/templates/pages/landingview.go`); the landing's copy does not
  change in this phase except where §7 says so (*gap report*: landing text equals
  `landingview.go` byte for byte, NBSP aside).
- **Board-internal labels are not card IDs.** The boards say "LP-6 (builder A)" and "LP-7 ·
  share cards / interactive states" — those are the design tool's own step names. The plan's
  LP-06/LP-07 are different things.
- **Links between boards** point at `Product.dc.html`, `Security.dc.html#proofs`,
  `Signup.dc.html`, `Legal.dc.html`, `SignIn.dc.html` … The last three are panel boards and live
  in [`../panel-redesign/boards/`](../panel-redesign/boards/). Every such link maps to a route in
  §3.
- **`./support.js`** (the design tool's runtime) and the **Google Fonts** `<link>` are not in this
  folder and are never ported: the boards do not render standalone outside the design tool, and
  the product self-hosts its fonts (`web/static/fonts/`, `@font-face` at `input.css:26-81`).
- Boards load Space Grotesk at **500** (`.navlink`, `.heroCreed`); the product ships 400/700 only
  (`web/static/fonts/README.md`) — map 500 to 400 or 700, never add a weight.

**Never copy an inline style.** Every public page is served with `style-src 'self'` and no
`'unsafe-inline'` (§4), so a `style=` attribute is refused by the browser — the existing landing
already measured that (comment above `.lp`, `input.css:1745-1754`). Board pages carry 86 (`T-Pricing`) to 641
(`States-Public`) `style=` attributes each, counted outside their `<script>`/`<style>` blocks
(LP-00 re-measured after review).

---

## 2. Decisions — binding

### The user's decisions (2026-10-21)

- **K1 — Real company and venue names stay public.** "Kebab Factory Ltd.", "Kebab Manufacturing
  Co." and the venue/department lists on ForRestaurants/ForProduction (and the landing trust
  strip) are published as drawn. Their tables must equal the demo seed (*gap report*:
  `test/fixtures/seed.sql:139-172, 198-211`); the "from our demo data" label stays.
- **K2 — Hosting sentence: "Hosted in the EU (Germany)".** The Security board's "Hosted in
  Germany" and "Taptime's production server runs in Falkenstein, Germany." are rewritten; no
  town, data-centre or provider name is published. Source of the fact: `deploy/README.md:9`
  (node `k8s-1.fsn1.private`, Hetzner fsn1) and `:6800-6801` ("fsn1 = Hetzner Falkenstein,
  Almanya").
- **K3 — Share images are PNGs exported from the ShareCards canvas** and committed under
  `web/static/og/`. No new dependency, no Go image generation.
- **K4 — A public 404 page is added**, in the new chrome: header + footer + one sentence + a link
  home. §8 lists what that touches.
- **Order:** the public site first, then the operator surface (M13).

### The orchestrator's decisions (recommended, recorded with their reasons)

| # | Decision | Why |
|---|---|---|
| O1 | Product's "Taptime guarantees" heading and "The Taptime guarantees come first and cannot be switched off." are **reworded** (suggested: "Taptime's guardrails" / "Taptime's guardrails come first and cannot be switched off."). No regex exception. | `unmeasuredNumber` (`internal/handler/marketing_test.go:985-988`) refuses `\bguarantee(?:d\|s)?\b` on every `marketingURLs()` page; "guardrail" is the product's own word (`sys:*`, CLAUDE.md §5). |
| O2 | The landing's animations **are removed**: the hero's `tapring` / `printout` (`.lp .scene.play …`, `input.css:1862-1868`, and `.lp .tapPulse` :1861, which exists only for `tapring`, their `reduce` rules :1988-1989, the `play` class at landing.templ:197) and — orchestrator decision (follow the boards, which draw none) — the demo dialog's `demoIn` / `demoFade` (`input.css:2054-2062`). | Follow the board; no animation left means no reduced-motion rule to get wrong. ⚠️ `TestLanding_ReducedMotionLeavesTheDocketVisible` is **not** affected: the `docket-print` it guards is used today only by the sign-up wizard (`signup.templ:583`; measured), not by the landing — its comment calling it "the landing page's ONE animation" (`input.css:948`) is stale. The test stays. Removing the hero animation also means removing the `clip-path: inset(0 0 100% 0)` that hides `.lp .lp-docket` by default (`input.css:1866`; only `printout` or the `reduce` rule :1989 reveals it) — LP-03. |
| O3 | **CSS stays `.lp`-scoped plain CSS in `input.css`** (outside `@layer`), as today. | §5. |
| O4 | Every new page enters the **fact-pinning** regime and `marketingURLs()`. | §6. |
| O5 | Signup, Legal and the API guide move onto the **new public header/footer** (States-Public draws them in it). | LP-11. |
| O6 | Routes, from ShareCards: `/product`, `/pricing`, `/security`, `/for/restaurants`, `/for/production`. | §3. |
| O7 | Pricing's "Frozen" stamp is the panel's precedent: `<span class="stamp stamp--approved">` with the word FROZEN (`web/templates/pages/billing.templ:120-124`). | The skill's stamp vocabulary is verdicts; the panel already stamps a frozen month this way, so no new stamp word is invented. |
| O8 | Nav/menu/footer **links arrive with their routes**, never before. The old "fits + more links" card is dissolved into the page cards. | `TestMarketing_EveryInternalLinkResolves` fetches every `href` on every public URL and wants 200 (`marketing_test.go:759-809`). Plan §"Sıralama". |
| O9 | **Port deviations from the Landing boards (LP-03, accepted at review).** (a) Below 1200px the hero ticket's stamp sits on a line of its own, under the CLOCK IN / CLOCK OUT tag, for every record; M-Landing and T-Landing draw it beside the tag for Maria. (b) The hero scene carries a **pause button** under the phone ("Pause" / "Play"; `#heroPause`), which no board draws. | (a) landing.js rotates four records, and "CLOCK OUT" beside the stamp does not fit below 1200px: the ticket wrapped and the page below the scene moved 28px on every tick (measured at 390 and 834). A stamp line for every record keeps one height. (b) WCAG 2.2.2: content that changes on its own must be stoppable. The button ships `[hidden]` and the script reveals it; a mouse over the scene pauses quietly; keyboard focus entering it pauses until the button resumes; under reduced motion nothing rotates and the button stays hidden. Pinned by `TestLandingHero_TheTicketKeepsItsHeightForEveryRecord` and `TestLandingHero_ThePauseMachineIsTheTable`. |

---

## 3. Board ↔ route ↔ code map

| Boards (desk · tablet · phone) | Route | Handler (`internal/handler/marketing.go`) | Templ | Card |
|---|---|---|---|---|
| shared chrome (every board's header, menu, phone bar, footer) | every public route | `render` :507 / `renderWith` :515 | new chrome component in `web/templates/pages/` (today: `landingNav` landing.templ:110, `landingFooter` :439; `marketingChromeWith` :635 for legal/signup/guide) | LP-02, LP-11 |
| `Landing` · `T-Landing` · `M-Landing` | `GET`/`HEAD /` | `Landing` :421 (mounted :369-370) | `Landing` landing.templ:89 and its sections :110-479 | LP-03 … LP-05 |
| `LandingDemo` · `M-LandingDemo` | the `<dialog>` on `/` | same | `landingDemoDialog` landing.templ:481 | LP-03 (style only; content unchanged) |
| `Pricing` · `T-Pricing` · `M-Pricing` | new `GET`/`HEAD /pricing` | new | new | LP-06 |
| `Security` · `T-Security` · `M-Security` | new `GET`/`HEAD /security` (anchor `#proofs`) | new | new | LP-07 |
| `Product` · `T-Product` · `M-Product` | new `GET`/`HEAD /product` | new | new | LP-08 |
| `ForRestaurants` · `T-` · `M-` | new `GET`/`HEAD /for/restaurants` | new | new | LP-09 |
| `ForProduction` · `T-` · `M-` | new `GET`/`HEAD /for/production` | new | new | LP-10 |
| (panel boards `Signup`, `Legal`) + this chrome | `/signup`, `/legal/*`, `/docs/api`, `/docs/api/tr` | `signup.go`, `legal` :441, `apiGuide` :402 | `signup.templ:42`, `legal.templ:50`, `apiguide.templ:26` | LP-11 |
| — (K4; no board — chrome + one sentence) | any unrouted path on the customer host (§8) | new | new | LP-12 |
| `ShareCards` | `<head>` of 9 pages + `web/static/og/*.png` | `NewMarketing` :209 | `layout.marketingDocument` base.templ:473 | LP-13 |
| `States-Public` · `M-States-Public` | — (state matrix for every public control) | — | CSS | LP-01, LP-14 |

Today the public routes are `/`, the four legal documents (`pages.LegalPages`, landingview.go:957)
and `/docs/api(/tr)` (marketing.go:368-386). There is **no public 404** (the router answers
net/http's plain text).

**Registering a route.** Mount new routes so `internal/handler/admincookiepath_test.go`'s
employee-route derivation can read them (literal or resolvable named path; a new `range` over a
table needs that derivation extended in the same commit and `TestEmployeeRoutes_DerivationIsNotVacuous`
kept honest). Every new route answers **HEAD as well as GET** (Mount's comment, marketing.go:336-367).

---

## 4. Constraints every public page inherits

1. **CSP.** `marketingCSP` (marketing.go:579): `default-src 'none'; style-src 'self';
   font-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'`. Only `/` adds
   `script-src 'self'` (`landingCSP` :600; `landingCSPFor` :627 adds `media-src`/`img-src` only when
   the demo files are embedded). The new pages are **script-free** and get `marketingCSP` as is.
   There is **no `img-src`**: no `<img>`, no CSS background image, no `data:` URI; illustrations
   and icons are inline SVG. `TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse` holds this.
2. **No inline style** (§1). Board `style=` → a class in the `.lp` block of `input.css`.
   Data-driven values on the boards (tilts, bar heights, positions) become a **fixed set of
   classes** chosen by the template, never a computed `style=`.
   `TestLanding_UsesOnlyPaletteTokens` refuses a raw hex in `landing.templ` and any `style=` /
   hex on the rendered `/`; extend it to every new templ file and page.
3. **Fonts self-hosted** (`/static/fonts/`); no request to any other host
   (`TestMarketing_ReachesNoThirdParty`, which also scans `content="…//…"` — §9 LP-13).
4. **The user-facing brand is Taptime** (`TestLanding_ShowsNoUserFacingInternalName`; redline
   B1 scans `web/templates` and `internal`).
5. **Same bytes for every visitor, cacheable** (`public, max-age=300`, `marketingCacheControl` marketing.go:640), no
   cookie read, no DB read (`TestMarketing_HandlerHoldsNoStatefulDependency`,
   `TestMarketing_TheSameBytesForEveryVisitor`, `TestMarketing_IsCacheableAndCarriesNoPanelHeaders`).
6. **Copy rules on every `marketingURLs()` page:** no unmeasured number/guarantee/uptime
   (`TestLanding_MakesNoUnmeasuredNumericClaim`), no "no API" sentence while `/v1` is mounted
   (`TestLanding_ClaimsNoAbsentAPIWhileV1IsMounted`), no demo dialog off `/`
   (`TestLandingDemo_NoOtherScreenOnThePublicSurfaceCarriesTheDialog`), no "better than"
   (`superiorityClaim`, marketing_test.go:1011-1013 — today `/` only; apply it to the new pages).
7. **Red lines in copy:** §4.1 (no biometric data) and §4.2 (position read only at the press) are
   stated in the footer in the exact wording redline R1 already recognises (comment above
   `marketingChrome`, landing.templ:564-573 — keep the denial on one line).
8. **Touch targets ≥ 44 px**; status never colour alone (stamps carry words; `.chk` carries a
   glyph); one `<h1>` per page; skip link to `#main` (as `marketingChromeWith` has).
9. **Tailwind scans only `.templ` and `web/static/js`** — no class name built in Go, no utility
   name written in a templ comment. `.lp` rules are plain CSS and are not tree-shaken.

---

## 5. CSS strategy — the `.lp` block

The existing landing CSS is one plain-CSS block, every selector under `.lp`, outside `@layer`,
after `@tailwind utilities` (`input.css:1714-2062`: the reference block :1714-1992, then the demo
dialog block :1993-2062, also `.lp`-scoped; header comment :1714-1761 records why:
bare selectors would reach the panel, tap screen and wizard; `.lp-` prefixes where the name was
taken — `.lp-btn`, `.lp-docket`, `.lp-ledger`). Continue it:

- **Every public page's own content renders inside one `.lp` root** and its classes are `.lp …`
  rules. Names the board uses that the product already owns (`.btn`, `.docket`, `.stamp`, `.card`?)
  get the `lp-` prefix or are scoped so no leftover of the product component leaks (the reason
  `.lp-docket` exists). Check every board class (§6) against `input.css` before naming it.
- **⚠️ `.lp *` beats Tailwind utilities.** `.lp * { margin: 0; padding: 0; }` (:1793) has the same
  specificity as a utility class and comes later in the file, so inside a `.lp` root every
  Tailwind spacing utility loses (inferred from source order — `@tailwind utilities` is line 3,
  the `.lp` block line 1714; **not measured in a browser**). Consequence for LP-11: Signup, Legal
  and the API guide keep their Tailwind bodies, so they must **not** be wrapped in `.lp`; the
  shared chrome must render correctly outside a page-wide `.lp` root (its header and footer as
  their own roots, or chrome classes defined without the reset). Measure this before choosing.
- **Breakpoints.** Boards: phone 390, tablet 834, desktop 1440. Use the product's widths: base =
  phone, `@media (min-width: 768px)` = tablet (Tailwind `md`), `@media (min-width: 1200px)` =
  desktop (`screens.desk`, tailwind.config.js:30). Today's `.lp` uses max-width queries at 720,
  820, 860, 900 and 960 px (`input.css` `.lp` block); a card that restyles a section moves that
  section to the shared widths. The boards' one own query, `@media (max-width:599px)
  {html{scroll-padding-bottom:80px}}`, is the phone bar's clearance — keep its intent (below
  768) rather than its number. Acceptance everywhere: no horizontal page scroll at 390 / 834 /
  1200 / 1440.
- **`html { scroll-padding-top: 72px }`** (board) under `html:has(.lp)` like the existing
  `scroll-behavior` rule (:1794), so anchor jumps clear the sticky header.

---

## 6. Token and component inventory

**Palette** (CLAUDE.md §9 + three lites already in `tailwind.config.js`): `ink #152219` ·
`porcelain #EDF0EA` · `paper #FFFDF4` · `tappa-green #1F5C41` · `green-lite #E1EDE6` ·
`saffron #D98E2B` · `saffron-lite #F7EBD6` · `tomato #BE3D2A` · `line #C9D2C8`; plus `--mint
#8FD3AF`, a `.lp` token only (`input.css:1780`, the plaque wave). The `.lp` block's `--ink-soft
#3D4F44` and `--green-deep` are not used by the boards (*gap report*); delete them only when no
rule reads them. Board rgba values are token + alpha: `rgba(21,34,25,.7/.82/.85/.24/.12/.08)` =
ink at that alpha; `rgba(31,92,65,.1/.22)` = green; `rgba(217,142,43,.1)` = saffron;
`rgba(190,61,42,.1)` = tomato.

**Board classes** (from the boards' `<style>` blocks; present on all 23 unless noted):

| Board class | Product name / note |
|---|---|
| `.btn`, `.btn--green`, `.btn--ghost` | 48 px, radius 12; green: paper on green + soft shadow, hover ink; ghost: 2 px ink border, hover inverts. Product `.btn` exists → `lp-` prefixed (today `.lp-btn` input.css:1821, `.lp-btn-ghost` :1831). |
| `.hbtn` | header "Start free", 44 px |
| `.navlink`, `.navlink[aria-current]` | header links; current = ink 700 + 2 px green underline (inset shadow) |
| `details.menu`, `.mopen`/`.mclose`, `.mlink`, `.mlink[aria-current]` | phone menu, native `<details>`, no JS; open summary = "Close" on ink; current = green text + 3 px green bar |
| `.mbar` | sticky bottom "Start free" on every public phone page **except Signup** |
| `.fcol`, `.flink` | footer columns (Product 6 links, Legal 4) and mono footer links |
| `.band--paper`, `.band--ink`, `.band--porcelain` | section grounds; inside `--paper`, cards/dockets/qa take an `ink/24` border; focus ring turns paper on ink and green |
| `.card`, `.fitcard`, `.ico` | 18 px card; whole-card link (landing fits) darkening its border on hover; 44 px green-lite icon tile |
| `.more` | "next step" link, one per section end ("Pricing in detail →", "How each proof works →", "Take the product tour →") |
| `details.qa`, `.pl` | FAQ item; first open; `+` rotates to `×` |
| `.eyebrow`, `.lbl`, `.mono` | mono kickers/labels |
| `.docket` (board), `.tapdocket` | board `.docket` has only a bottom perforation and differs from the product's `.docket` → use the existing `.lp-docket` or a new `lp-` name; `.tapdocket` = the tap-screen mock's ticket (both edges perforated) |
| `.stamp`, `--ok/--flag/--rej/--ign`, `--sm` | product: `.stamp--approved/--flagged/--rejected/--ignored` (input.css; word always ink — `TestCompiledCSS_StampWordIsInk`); board tilts −6/−3/−10/−4°; `--sm` is a 9.5–10 px variant (Security, States, For*) |
| `.chk`, `--ok/--no/--na`, `.tick` | Security and For* only: 18 px check squares (paper on green / paper on tomato / ink on line) with a glyph; `.tick` = timeline hairline |
| `.cell`, `.na` | States-Public / ShareCards / Product only: the state-matrix cell — board furniture, **not ported** |

**Focus ring** (States-Public): one ring for every public control, `2px solid` ink with `2px`
offset on porcelain/paper, **paper** inside `.band--ink` and on green. It replaces `.lp`'s 3 px
saffron ring (`input.css:1799`), which measures 2.32:1 on porcelain (*gap report*; the board
prints 2.32:1 porcelain / 2.62:1 paper and 14.32 / 16.17 / 7.73 / 16.17 for the new pairs).
Pressed = `:active`; disabled exists only on form submits (Signup); visited links keep the
default colour.

---

## 7. Fact pinning — how the landing does it, and how new pages join

**The mechanism today** (read `web/templates/pages/landingview.go:124-147` and the three test
files before writing a sentence):

- Every claim-carrying value is data in `landingview.go` that names what it rests on in one of
  **three vocabularies**: `Source` (engine facts — policy statement, query, method; closed by
  `internal/handler/marketing_claims_test.go`), `Anchor` (schema/domain facts — closed in
  `marketing_test.go`), `Fact` (product facts for steps, ledger, price card, FAQ, ledes — closed in
  `marketing_facts_test.go`; a Fact that *is* a Source/Anchor fact delegates to that derivation).
  Values are `Line{Text, Facts}`, `Step`, `ComparisonRow`, `Evidence`, `Audience`/`Claim`,
  `Question` — rendered from data, never typed in the template.
- **Closure** (one per vocabulary): every declared constant must be **derived** from the product
  (a function reading code, schema, migrations, CSS or config — never the page's own sentence)
  **and claimed** by at least one rendered value; every derivation must hold today. Where the fact
  is a decision recorded only in a repository document, the derivation reads **that document** for
  the recorded phrase — precedent `FactPlaquesIncludedAndReplacedFree`, whose derivation matches
  `plaquesIncludedRE` against `docs/handoff.md` §11 (`marketing_facts_test.go:614-619`). U8 (§10)
  pins the hosting region the same way, against `deploy/README.md`. What is never done is
  matching the marketing sentence against itself.
  `TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed`,
  `TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed`,
  `TestLandingAudiences_EveryClaimRestsOnAProductAnchor`. Each has a negative control
  (`TestFactMechanisms_SayNoWhenTheFactIsAbsent`, `TestSourceMechanisms_SayNoWhenTheFactIsAbsent`,
  `TestAnchorMechanisms_SayNoWhenTheCapabilityIsAbsent`).
- **Rendering half:** every value carries ≥ 1 pin **and** reaches the visitor
  (`TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact`,
  `TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact`,
  `TestLandingFAQ_EveryAnswerRestsOnAProductFact`, `TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact`,
  `TestLandingEvidence_EveryProofLabelIsRendered`, `TestLandingSteps_EveryStepIsRenderedInOrder`,
  `TestLandingAudiences_EveryClaimIsActuallyRendered`). Some tests also pin exact wording and
  ban phrases (the FAQ's §5 sentences; "live headcount", "import your staff", "under 10 minutes" …).
- **Scanners over every `marketingURLs()` page** (§4.6).
- The written limit (landingview.go:143-147): naming a pin does not make a sentence true; this is a
  ratchet against drift, and every sentence is still read against the product by a person.

**How a new page joins** (LP-06 builds this once; LP-07 … LP-10 reuse it):

1. **Copy is data.** Each page's sentences live in a view file beside `landingview.go` (e.g.
   `pricingview.go`) as `Line` / `Question` / page-specific structs with `Facts []Fact` (or
   `Sources`/`Anchors`), rendered by the template. A new constant goes into the existing
   vocabulary blocks — `declaredFacts` (`marketing_facts_test.go:682`) reads **only**
   `landingview.go`, so either declare there or widen the reader in the same commit.
2. **Claims are counted across all pages.** `claimedFacts()` (:712) and `landingLines()` (:698)
   only see landing values. Widen them to every public page's values, or a Fact used only on
   `/security` fails closure as "no sentence rests on it".
3. **Rendering is checked on the page that renders it.** Generalise `renderedLandingText`
   (`marketing_claims_test.go:54`) to a URL, and assert each page's values on its own URL.
4. **The route enters the URL lists.** `marketingURLs()` (`marketing_test.go:119`) derives from
   `/` + `pages.LegalPages`; give the new pages a table both `Mount` and the test read (so a page
   cannot exist without a route or the other way round — the shape `LegalPages` already has).
   From then on the §4 scanners and `TestMarketing_EveryInternalLinkResolves` cover the page.
5. **Sample data is not a claim, and is labelled.** Board names/times on mock screens (Pass,
   Spike, Roll minis, tap mock, timelines) are invented and carry no product statement. Tables
   that are **real** (For* venues/departments/shift times — K1) are pinned to
   `test/fixtures/seed.sql` by a test, and keep "from our demo data".
6. **Numbers are derived, not typed:** "eleven guardrails" from the policy package's guardrail
   list, "150 m" / "60 seconds" from `config.go` defaults (*gap report* :405, :408;
   `SourceGPSRadiusDefault` landingview.go:187 already pins 150), "256-bit" from
   `internal/session/token.go` (*gap report* :58, :67), the price and the example from the
   existing constants (`publishedPricePerEmployeeMonth` marketing.go:655, `exampleHeadcount` :661, `foundingFreeMonths` :701).

---

## 8. The public 404 (K4) — what it touches

Not a board: header + footer + one sentence + "Back to the home page", status 404, `noindex`.
The router's 404 is **net/http's plain `404 page not found\n`, and several places depend on being
byte-identical to it**:

- `internal/httpx/apihost.go:30-48` and `operatorhost.go:104-125` answer foreign paths with
  `http.NotFound` — "the router's own 404" (apihost.go comment).
- The tenant theme and logo refusals are compared **byte for byte** with the router's answer to an
  unrouted path, so an unknown theme/logo is indistinguishable from no route:
  `brandThemeReference404` (`internal/handler/brandtheme_test.go:537-563`, request
  `GET /no/route/is/mounted/here`, host `wl5.test`), used by
  `TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders`; `TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer`
  (`brandlogo_test.go:545-561`).
- `TestAccessLog_UnroutedRequestStillGetsARecord` (`internal/httpx/requestlog_test.go`) — an
  unrouted request is logged without the caller's path.

So a global `r.NotFound(styledPage)` would (a) render the public chrome on the **operator host**
for unknown `/operator/…` paths (ADR 0020: a separate surface), (b) change the reference the brand
refusals are measured against. **Recommended shape** (LP-12 decides it with the security lens and
records it in an ADR note): the styled page only for `GET`/`HEAD` on the customer host, only for
paths **outside** the prefixes whose refusals must stay router-identical (`/brand/`, `/static/`,
`/operator`, `/v1`, the tap and panel prefixes — inventory them, do not guess); everything else
stays `http.NotFound`. Of the brand tests, only the theme reference can move, and only **if the 404
is bound at router level**: then `brandThemeReference404` and its siblings, which request
`/no/route/is/mounted/here`, would get the page, so they move to an unrouted path **inside**
`/brand/` (the theme route is `/brand/theme/{file}`, `internal/handler/brandtheme.go:15`), which
keeps the property they protect (a probe of `/brand/…` sees one answer). If the 404 is bound in
Marketing's `Mount`, they do not move (see below). The logo test compares against a fixed router-404 answer on `/t/logo/…` and
`/admin/brand/logo/…` (`brandlogo_test.go:545-561`) — unchanged as long as the tap and panel
prefixes are excluded. The theme reference sits in four places on the customer host
(`brandtheme_test.go:89, 512, 550, 602`; :712 is the operator host), and those tests build a router
**without** Marketing (:64, :704): a 404 bound in Marketing's `Mount` leaves them unchanged but no
longer describing production, so LP-12 then adds a test on the full composition; a 404 bound at
router level changes all four. Not measured yet: whether chi's `NotFound` can be scoped this way without a wrapper.

---

## 9. Share cards and meta (K3, LP-13)

- **Nine images**, 1200×630 PNG, `web/static/og/`: `landing`, `product`, `pricing`, `security`,
  `restaurants`, `production`, `signup`, `legal` (one for the four documents), `api`. The board
  names each page's `<title>`, `description` and source value. **No price in an image** (board
  rule: the price is derived, an image is not). Names in images are invented samples; KM
  department names are seed data (K1).
- **How the PNG is produced (K3):** exported from the ShareCards canvas in the user's design tool,
  1200×630 per card, and committed. There is no second route: if the builder cannot export, it
  asks for the user's export; no screenshot tool, no generator in the repo.
- **Head tags only in the marketing shells.** `documentHead` (base.templ:206) is shared by every
  shell (tap, activation, panel, operator); the tap/activation heads are pinned byte for byte by
  `TestUnbrandedScreens_AreByteIdenticalToTheGolden`. Add `description` / `og:*` /
  `twitter:card` through `marketingDocument` (base.templ:473) or a marketing-only parameter.
- **`og:image` wants an absolute URL**, the surface does not know its origin today
  (`MarketingTranslated`'s comment, base.templ:458-461) → it comes from `TAPPA_BASE_URL`
  (`config.go:276`), handed to `NewMarketing` (cmd/tappa/main.go:863). The signature change reaches every caller —
  10 call lines outside the definition (measured with `grep -rn --include='*.go' 'NewMarketing(' .`): `main.go:863`,
  `cmd/tappa/serving_test.go:52`, `cmd/tappa/approuter_test.go:221`,
  `internal/handler/marketing_test.go:91` (`marketingRouterFor`) and `:544`,
  `internal/handler/signup_test.go:981, 983`, `internal/handler/apiguidepage_test.go:212, 251`,
  `internal/handler/operator/op10_db_test.go:319` (the LP-13 card carries the same list). **`TestMarketing_ReachesNoThirdParty`'s scanner matches
  `content="https://…"`** (`externalReference`, marketing_test.go:569-573) → it needs an exemption
  for exactly the configured origin's `/static/og/` URLs, shaped like the existing OpenAPI
  exemption — a security-relevant change.

---

## 10. Claims on the boards that are not verified

Each must be **pinned** (a derivation in §7's regime) or **softened** before it ships. "Measured
in LP-00" means read in the code while writing this package; the builder still pins it.

| # | Board · sentence | Status | Where to measure / softer text |
|---|---|---|---|
| U1 | Security: "a **16-character** signature (CMAC)" | measured in LP-00: `cmacHexLen = 16 // 8-byte truncated SDM MAC` (`internal/sun/params.go:34`) | pin to `cmacHexLen` |
| U2 | Product (The Pass): "Pick any earlier day to read it in full." | partly: the records list parses a `date` query (`internal/handler/transactions.go:338`, `ledger.ParseDate`) | confirm the panel offers the day choice to a manager; else "Earlier days stay on the record; nothing is deleted." |
| U3 | Product (The Spike): "A note can go on the record with the decision." | partly: the decision POST reads `note` (`internal/handler/review.go:417`, `reviewNote` :525); column `transaction_reviews.note` (migration 00005:270) | confirm the review form renders a note field; else drop the sentence |
| U4 | Pricing FAQ: "Until it is frozen, correcting somebody's joining or leaving dates can still move it." | **not found**: `activated_at` is set with `COALESCE(e.activated_at, now())` (`db/queries/invites.sql:253`), `deactivated_at = COALESCE(deactivated_at, now())` (`employees.sql:722`); no edit path found (API upsert not checked) | measure every write of the two columns; else e.g. "Until it is frozen, its figures are recomputed whenever they are read." (pin to the freeze code) |
| U5 | Product (People): "The link is shown once, on the next screen. Taptime keeps no copy of it." | **partly false**: with `TAPPA_INVITE_DELIVERY=email` the link is mailed and not shown (`internal/handler/inviteemail.go:25-28`); the stored form is a hash (`resolve_invite_by_code_hash`, migration 00012:90) | e.g. "The invitation link is shown once, or sent to the person's own e-mail address. Taptime keeps only a hash of it." — pin both modes |
| U6 | Pricing FAQ: "anybody who was on the books for even one day of it" | **imprecise**: billing counts `[activated_at, deactivated_at)` overlapping the month (`db/queries/billing.sql:67-68` comment); invited-but-never-activated is not counted | e.g. "anybody who was active for at least one day of it. An invitation nobody accepted is not counted." |
| U7 | Product: "Taptime guarantees" (heading + bullet) | refused by the scanner | O1 rewording |
| U8 | Security: "Hosted in Germany", "…runs in Falkenstein, Germany." | K2 | "Hosted in the EU (Germany)"; pin to `deploy/README.md:6800-6801` |
| U9 | Security: "Five of the **eleven** guardrails are shown." | *gap report*: 11 `sys:*` | derive the count from the guardrail list |
| U10 | Product (The Spike): "Nobody can decide on their own record." | rule exists: `ErrSelfReview` (`internal/domain/review/review.go:88-89`, "unreachable today" — a reviewer is a panel user) | pin to the trigger/`ErrSelfReview` |
| U11 | Landing hero script: "KM Facility" | wrong: the seed calls it `KM Plant` (`test/fixtures/seed.sql:171`; `web/static/js/landing.js:21`) | boards already say KM Plant |
| U12 | ShareCards: "Source … (landingview.go:403 / :447 / :453 / :428 / :735)" | line numbers stale (`LandingHeroLede` is at :424 at `0e7c158`) | re-find by symbol |

The list is the gap report's plus what LP-00 found; it is **not exhaustive**. Every sentence on a
new page goes through §7, and the closure tests are the net — not this table.

---

## 11. Tests a port must keep green (or change deliberately, in the same commit)

| Test | What changes it | Card |
|---|---|---|
| `TestLandingNav_EveryLinkPointsAtASectionOnThePage` (`marketing_facts_test.go:975-1029`) | nav entries stop being in-page anchors once `/pricing` exists; its second half asserts the legal pages carry **no** landing nav and **no** `sticky` (:1015-1028) | LP-06 (first half), LP-11 (second half) — rewritten, assertion intent kept: every nav link resolves; the current page is marked |
| `TestLanding_ReducedMotionLeavesTheDocketVisible` (`marketing_test.go:2301`) | **nothing** — it guards the wizard's `docket-print` (`signup.templ:583`), not the hero (O2) | stays green in LP-03; LP-03 adds its own no-animation assertion for `.lp` |
| `TestLandingDemo_TheScriptDegradesInsteadOfBreaking`, `TestLandingDemo_TheSecondHeroButtonSaysWatchDemoAndOpensTheDialog`, `TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse` | hero markup and `landing.js`; the last one also **inverts its animation assertion** — today it requires `@media (prefers-reduced-motion:no-preference){.lp .demoDialog[open]{animation:` in the compiled CSS (`marketingdemo_test.go:165-168`); with the demo animation removed (O2, orchestrator decision (follow the boards)) it asserts the dialog has **no** animation, and its label / keyboard-close / 44 px assertions stay as they are. A rename moves every citation in the same commit. | LP-03 |
| `TestLanding_UsesOnlyPaletteTokens` | widened to new templ files and pages | LP-06 … LP-10 |
| `TestMarketing_EveryInternalLinkResolves` — **its link handling** | it fetches each `href` as written; `/#how` and `/security#proofs` reach chi as the path `/#how` and answer 404 (measured by the LP-00 reviewer). It must split the fragment, GET the path (200) **and** find the `id` on that page | LP-06 (LP-07 relies on it) |
| `TestMarketing_EveryInternalLinkResolves`, `TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse`, `TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders`, `TestMarketing_ReachesNoThirdParty` | new URLs join `publicSurfaceURLs()` | LP-06 … LP-10, LP-13 |
| `TestAPIGuidePages_AreTheGuideInTheMarketingChrome`, `TestAPIGuidePages_FitAPhone`, `TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce`, `TestSignupSurface_UsesNoUnanchoredCapabilityWord`, `TestSignupSurface_EverySignInLinkGoesToTheSignInPage` | the chrome moves (the signup surface refuses capability words nothing derives — a footer word like "report" or "export" arrives on its screens) | LP-11 |
| `TestUnbrandedScreens_AreByteIdenticalToTheGolden`, `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` | must **not** change in this phase; red = a shared component was touched | all |
| `TestEveryNamedTestExists` (`cmd/tappa/testnames_test.go`) | every `Test…` name written in any repo file must exist — describe a new test, let the builder name it | all |
