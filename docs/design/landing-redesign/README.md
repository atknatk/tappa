# Landing redesign — handoff for the port

This folder is the handoff package for porting the **public landing page** (`/`) from the approved
boards (2026-10-21) to templ + the landing's scoped CSS. A developer agent should be able to port the
page from this README and the M14 task cards without discovering surprises in the code.

- **Boards:** `docs/design/panel-redesign/boards/` — every canvas board lives there, and
  `canvas.json` lays them out (the public row is the note "Public site — landing & pages (desktop /
  tablet / mobile)", `t7`, at y 32260). **26 public boards** (LP-6 round, 2026-10-21):
  - `Landing.dc.html` (desktop, 1440, fluid, h 9177) · `T-Landing.dc.html` (834, h 10629) ·
    `M-Landing.dc.html` (390, h 14247);
  - `LandingDemo.dc.html` (1440×900) and `M-LandingDemo.dc.html` (390×844) — the demo dialog (header +
    hero only, no footer, by design);
  - `Product`, `Pricing`, `Security`, `ForRestaurants`, `ForProduction` × desktop / `T-` / `M-` — the LP-6
    pages (Part II);
  - `Signup`, `Legal` × desktop / `T-` / `M-` — re-headed twice: first with the ring lockup (§7), then
    with the LP-6 shared header and footer (Part II §12). Their body between header and footer is
    byte-identical to the approved boards (two pre-existing body defects fixed: Signup chips Plex Mono
    500 → 400, Legal cookie table curly quotes → ASCII).
  Sections 1–9 below were written for the first landing round; where Part II (§10–§19) changes them,
  Part II wins.
- **LP-6 claim inventories:** [`lp6-inventory-a.md`](lp6-inventory-a.md) (landing additions, `/product`,
  `/pricing`, shared chrome, re-head) and [`lp6-inventory-b.md`](lp6-inventory-b.md) (`/security`,
  `/for/restaurants`, `/for/production`, port feasibility) — every NEW CLAIM row with its code fact.
- **Task cards:** [`docs/plan/m14-landing-redesign.md`](../../plan/m14-landing-redesign.md)
  (M14-01 … M14-24, ordered, each independently shippable on the branch; M14-14 … M14-24 are LP-6).
- **Shared with the panel port** (by reference, not repeated here):
  [`docs/design/panel-redesign/README.md`](../panel-redesign/README.md) §1 (how to read a board —
  bindings, sample data, inline styles that must never be copied) and §4 (palette).

All `file:line` citations here refer to **HEAD `945b6b9`**. Lines move; re-find them by the symbol
named beside each one. Short names: `landing.templ` = `web/templates/pages/landing.templ`, `view` =
`web/templates/pages/landingview.go`, `css` = `web/static/css/input.css` (the `.lp` block starts at
`:1763` and runs to about `:2060`), `landing.js` = `web/static/js/landing.js`, handler =
`internal/handler/marketing.go`.

**How to read a landing board.** Same as the panel boards (panel README §1): a mock-up, not a template.
Inline `style="…"` everywhere (never copied — §3), `{{binding}}` placeholders, and a trailing
`<script type="text/x-dc">` with the sample data. Props: the three `Landing` boards take
`ticket` = `maria | kevin | anna | luke` (landing.js's four records); the two `LandingDemo` boards
take `state` = `soon` (what ships today) `| video`. Links on the boards are `href="#"` (board
convention); the port uses the real targets named below. `canvas.json` is kept in the repo's compact
single-line form (as it always was); a pretty-printed copy is the same layout, but keep the compact one so
its diff stays one line. The boards were generated from one source
outside the repo; treat them as read-only references, and where a board and this README disagree,
this README (which cites the code) wins.

---

## 1. What the landing is (read before drawing a pixel)

- **One public page on the product host**, rendered by `pages.Landing(v LandingView)`
  (`landing.templ:89`) inside `layout.MarketingWithScript(…, layout.RobotsPublic,
  "/static/js/landing.js")`. The handler is `internal/handler/marketing.go`; the page is the same bytes
  for every visitor (`TestMarketing_TheSameBytesForEveryVisitor`), cacheable, no session.
- **It is not the panel and not marketingChrome.** The landing has its own header (`landingNav`,
  `landing.templ:110`) and footer (`landingFooter`, `:439`), styled by the plain scoped CSS block
  `.lp { … }` in `input.css`. Signup, the legal pages and the API guide use `marketingChrome` /
  `marketingChromeWith` (`landing.templ:586`, `:635`) with Tailwind utilities. One stylesheet
  (`app.css`) carries both; the landing's rules are scoped under `.lp`.
- **It was a 1:1 port of the user's reference** (`docs/design/landing-reference-2026-09-12.html`;
  user 2026-09-14: *"birebir"*). Every claim-carrying sentence is a Go value in `landingview.go`, and
  each one rests on a product **Fact** / **Source** / **Anchor** that a test derives from the code and
  finds rendered (the D1 discipline of this repo: *code is the truth*).
- **One script:** `landing.js` — the hero ticket's rotation (3.8 s, off under reduced motion) and the
  demo dialog's opener. No request, cookie, storage or geolocation
  (`TestLandingDemo_TheScriptDegradesInsteadOfBreaking`).
- Section order today: header → hero → pilot strip → how it works (`#how`) → why switch (comparison)
  → security (`#security`) → built for both shapes → pricing (`#pricing`) → FAQ → CTA (`#pilot`) →
  footer, plus `<dialog id="demoDialog">`.

---

## 2. The user's decisions — binding

### LP-0 — Scope B: "fix + align" (2026-10-21, *"B: Düzelt + uyum"*)

The 2026-09-14 *"birebir"* (1:1 with the reference) is **softened**, not dropped: keep the
reference's **structure, order, voice and every claim**; fix the defects (LP-2); align the look with
the redesigned panel and marketing chrome (LP-3); add a panel showcase and the employee's phone screen
(LP-4). It is **not** a from-scratch redesign.

### LP-1 — Copy (D1 "code is the truth" applies)

- Every claim-carrying string is a value in `landingview.go` pinned by tests (§4). The boards use those
  strings **verbatim**. The reference's furniture (headings, eyebrows, button labels, strip, footer
  lines) stays verbatim too.
- New copy is allowed **only** for the panel showcase and the phone screen. It must be true of the
  product as built (real screen names: The Pass, The Spike, The Roll, People, Venues & plaques; the tap
  screen's real strings from `web/templates/pages/tap.templ`), and it is marked **NEW CLAIM** below →
  in the port each becomes a `landingview.go` value with its Fact and a test (§6.7, M14-08).
- **Never:** invented features; numbers that are not derived (price, free months and the example
  monthly come from `LandingView`); testimonials; customer logos beyond the existing pilot strip;
  "Book a pilot".

### LP-2 — Defects fixed on the boards

1. Mobile keeps **"Sign in"** visible in the header (today `css` `@media (max-width: 720px) { .lp
   .navLinks a:not(.lp-btn) { display: none; } }` hides it with the anchors).
2. Hero scene: nothing overlaps the plaque's "Tap here to clock in / out"; the APPROVED stamp never
   covers a docket row ("On site  IP + GPS ✓" must read). Today `.lp-docket` is absolutely positioned
   over the plaque and `.d-stamp` sits over the rows.
3. The comparison table keeps **both** columns at every width (phone: each row stacks
   aspect / ✗ device / ✓ Taptime).
4. **No emoji anywhere** (step icons, footer flag): line icons in the panel's hand (§8.3). The footer
   reads "© 2026 Taptime · Malta".
5. **IBM Plex Mono 400/700 only** (no 500/600 mono anywhere); the creed stays readable.

### LP-3 — Alignment with the redesign

- **One marketing header** = the Signup/Legal boards' header (green ring mark + "taptime" + PUNCHLESS,
  linking to `/`), with the landing's anchors (How it works · Security · Pricing) + Sign in + Start
  free. Same footer family as Signup/Legal, keeping the landing's two lines + legal links + API docs.
- **D2 corners:** docket and stamp sharp; cards, buttons, chips rounded.
- **Palette tokens only** (ink, porcelain, tappa-green, saffron, tomato, paper, line + opacity; the
  existing `--mint` on `.lp` is allowed for the plaque wave and ink-section accents) — §5.
- **Desktop width use:** FAQ in two columns (heading/lede left, list right); the privacy note spans the
  grid.

### LP-4 — Additions

- **Hero:** the employee's phone (the tap screen — one screen, one button — drawn exactly as
  `tap.templ` shows it, **not changed**) beside the plaque, and the docket that prints.
- **New section "The panel"** (`#panel`, placed **after Security** — §6.7 says why): miniatures of
  The Pass, The Spike and The Roll, drawn in HTML/CSS (no `img-src` — no `<img>`, no screenshots), sample
  data invented and labelled as such.
- **Demo dialog** drawn as a state: no video yet (`soon`) / video present (`video`).

### LP-5 — Constraints for the port

CSP `style-src 'self'` (no inline style in templ), no `img-src` (inline SVG only), script only
`landing.js`, no new JS file, no third-party request, self-hosted fonts. Breakpoints: 1440 fluid / 834 /
390 boards (§8.5 for the CSS breakpoints).

### Later user decisions (fix round, 2026-10-21)

- **R-1 — landing.js gains the swap (rotation incl. the plaque line).** Inside the **existing** 3.8 s
  interval, `landing.js` writes **three more** text nodes, so the ticket, the phone and the plaque always
  show **one** record: the phone's greeting name (`pName`), the phone's venue (`pLoc`) and the plaque's
  venue · uid line (`pPlaque`). Same file, no new file, nothing else new (table in §6.2). This
  overrides LP-4's "no new JS" for these three writes only.
- **R-2 — The FAQ's API wording ships separately.** **Update 2026-10-21: MERGED to `main`** —
  PR atknatk/tappa#6, merge commit `0e7c158`. M14-10 no longer depends on an unmerged branch; it needs
  `main` merged into the work branch (`claude/admiring-brown-jw8hf1` did not contain `0e7c158` when this
  note was written). The history below is kept for the record. Branch **`claude/fix-landing-faq-api`**;
  it **started** at `1054fea` (*fix(landing): name
  the integration API in the FAQ*, on top of `ab85193`) and already has follow-ups (`88e5379`, *widen the
  no-API scan and derive holding webhooks*); it may get more. It rewrites FAQ Q4 and Q5 (§6.10), retires
  the `FactNoIntegrationAPI` tripwire (it only scanned `/api…` literals, so it never saw `/v1`) and adds
  three API Facts plus a scanner test that fails while a public page says "no API" and `/v1` is mounted
  (the Landing-prefixed `…ClaimsNoAbsentAPIWhileV1IsMounted` test — spelled without its prefix here
  because `TestEveryNamedTestExists` refuses a test name that does not exist on this branch yet).
  **M14 does not re-implement it.** M14-10 (FAQ) needs it in the work branch (merge `main`, which carries
  `0e7c158`), and it should be in **before M14-08** too: the FAQ fix and the panel showcase both edit `landingview.go` (Facts) and
  `internal/handler/marketing_facts_test.go` — merging it first turns a two-sided conflict into a rebase
  of M14-08 alone. The same holds for the LP-6 cards that add Facts (M14-15 … M14-22).

---

## 3. Constraints every landing change inherits

1. **CSP unchanged.** `landingCSP` = marketingCSP + `script-src 'self'`; `media-src`/`img-src` are
   named only when the build carries the demo video
   (`TestLandingDemo_PolicyNamesMediaOnlyWhenTheBuildCarriesTheDemo`). Every drawing is inline SVG or
   CSS. No `data:` URI, no CSS background image.
2. **No inline style, no hex in the template.** `TestLanding_UsesOnlyPaletteTokens` (no hex in
   `landing.templ` or the rendered page; no ` style=`) and `TestTemplates_CarryNoInlineStyle`. Every
   board style becomes a class in the `.lp` block. Data-driven geometry (docket tilts, Roll cells, day
   squares, the 375 → N phone scale) becomes **fixed classes** per state / per breakpoint.
3. **No new JavaScript** beyond R-1's three `textContent` writes. Nothing in landing.js may null-check
   its way around a missing node — the new `pName`/`pLoc`/`pPlaque` elements are **unconditional** in the
   markup. Under reduced motion there is no timer, so the first record shows everywhere, as today.
4. **Fonts:** Space Grotesk (variable) + IBM Plex Mono **400/700**, self-hosted (`web/static/fonts`).
   Adding a weight is a fonts README + licence change — not in scope.
5. **One `<h1>`** on the page (the hero's). The phone's "Hello {name}" is a `<p>`/`<span>`, never a
   heading, and never `pages.TapHeading` (it emits an `<h1>` and is the live tap screen's component).
6. **The employee screens do not change** (CLAUDE.md §9): `tap.templ`, `result.templ`,
   `activate.templ`, `components.*`, `layout.*` keep their bytes
   (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`, `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`).
   The landing draws a **copy** of the tap screen.
7. **One `<nav>` first.** `TestLandingNav_EveryLinkPointsAtASectionOnThePage` reads the **first**
   `<nav>…</nav>` on the page and requires the three anchors + the signup and sign-in links in it. Build
   the phone header with that same nav and CSS-hide the anchors; never add a second nav before it.
8. **No unmeasured number** (`TestLanding_MakesNoUnmeasuredNumericClaim`, regex at
   `internal/handler/marketing_test.go:985`): no `N %`, no `99`/`100`, no "uptime", "SLA",
   "guarantee", "never fails". It reads the whole page — the panel miniatures' sample data too, so no
   TRUST 100.
9. **No internal name** on the page (`TestLanding_ShowsNoUserFacingInternalName`).
10. **A new `.templ` file must be classified.** If the port splits a component out of `landing.templ`
    (e.g. the miniatures), add it to `nonPanelTemplates` in `internal/handler/panelsource_test.go`
    (`:74` classifies `pages/landing.templ` as "marketing") with the same reason.
11. **Tailwind** scans `.templ` and `web/static/js`; the `.lp` block is plain scoped CSS, so its class
    names need no Tailwind compile. A Tailwind utility written in `landing.templ` does compile — keep the
    landing on `.lp` classes, except inside `marketingChromeWith` (Tailwind already).
12. **Tests during a card:** only the changed tests with `-run`, plus `gofmt`, build and
    `scripts/redline-check.sh`; the full `-race` suite and `make check` once, before the commit
    (CLAUDE.md §10). `make gen` + `make css` after every `.templ` / `input.css` edit. The committed
    generated files are the `*_templ.go` files only; `web/static/css/app.css` is gitignored and built by
    `make css` (CI and the image build it), so it is never committed.

---

## 4. Tests that pin landing markup and copy (keep green, or change deliberately)

**`internal/handler/marketing_claims_test.go`**
`TestLandingEvidence_EveryCardRestsOnAProductSource` ·
`TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed` ·
`TestSourceMechanisms_SayNoWhenTheFactIsAbsent` · `TestLandingSteps_EveryStepIsRenderedInOrder` ·
`TestLandingEvidence_EveryPieceIsRendered` · `TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer`

**`internal/handler/marketing_facts_test.go`**
`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` (every new NEW-CLAIM Fact must be both derived
and claimed) · `TestLandingFAQ_EveryAnswerRestsOnAProductFact` ·
`TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact` ·
`TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact` · `TestLandingSteps_EveryStepRestsOnAProductFact` ·
`TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact` · `TestLandingEvidence_EveryProofLabelIsRendered` ·
`TestLandingNav_EveryLinkPointsAtASectionOnThePage` · `TestLanding_ShowsNoUserFacingInternalName` ·
`TestLanding_UsesOnlyPaletteTokens` · `TestFactMechanisms_SayNoWhenTheFactIsAbsent`

**`internal/handler/marketing_test.go`**
`TestMarketing_HandlerHoldsNoStatefulDependency` · `TestMarketing_TheSameBytesForEveryVisitor` ·
`TestMarketing_IsCacheableAndCarriesNoPanelHeaders` · `TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse` ·
`TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders` · `TestMarketing_AVisitorLeavingIsNotAnError` ·
`TestMarketing_ReachesNoThirdParty` · `TestExternalReferenceScanner_CatchesWhatItExistsToCatch` ·
`TestMarketing_EveryInternalLinkResolves` (a new `#panel` link must have its id) ·
`TestMarketing_MountsEveryLegalDocumentAndLinksToIt` · `TestLanding_PriceMatchesTheSchemaItIsCharged` ·
`TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount` · `TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem` ·
`TestLanding_CarriesTheSlogan` · `TestLanding_MakesNoUnmeasuredNumericClaim` ·
`TestLanding_ComparesMechanismsAndDeclaresNoWinner` · `TestClaimScanners_RejectTheThingsTheyExistToReject` ·
`TestLanding_OffersTheWizardItMounts` · `TestAnchorMechanisms_SayNoWhenTheCapabilityIsAbsent` ·
`TestLandingAudiences_EveryClaimRestsOnAProductAnchor` · `TestLandingAudiences_EveryClaimIsActuallyRendered` ·
`TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist` ·
`TestLanding_ReducedMotionLeavesTheDocketVisible` (today it scans only signup's `docket-print`; M14-04 extends it to the landing) ·
`TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce` (legal pages wear marketingChrome)

**`internal/handler/marketingdemo_test.go`** — the opener `<a id="demoOpen" href="#how">`, the
`<dialog id="demoDialog" class="demoDialog" aria-labelledby="demoDialogTitle">`, the compiled-CSS
animation string, the empty state, the video/poster, the media policy, no dialog elsewhere, the script:
`TestLandingDemo_TheSecondHeroButtonSaysWatchDemoAndOpensTheDialog` ·
`TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse` ·
`TestLandingDemo_WithNoRecordingTheDialogSaysSoAndRendersNoPlayer` ·
`TestLandingDemo_WithARecordingTheDialogRendersThePlayerAndItsSource` ·
`TestLandingDemo_TheServedPageAgreesWithTheEmbeddedAssetTree` ·
`TestStaticAssetURL_AnswersOnlyForAFileThatIsActuallyThere` ·
`TestLandingDemo_PolicyNamesMediaOnlyWhenTheBuildCarriesTheDemo` ·
`TestLandingDemo_NoOtherScreenOnThePublicSurfaceCarriesTheDialog` ·
`TestLandingDemo_TheScriptDegradesInsteadOfBreaking` (reads landing.js, which now also writes
`pName`/`pLoc`/`pPlaque`)

**Elsewhere**
- `TestTemplates_CarryNoInlineStyle` (`internal/handler/inlinestyle_test.go`).
- `TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel` and
  `TestAPIGuidePages_AreTheGuideInTheMarketingChrome` (`internal/handler/apiguidepage_test.go`) — the
  landing footer's "API docs" link and marketingChrome's.
- `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` (`internal/handler/panelbrand_golden_test.go`,
  the unbranded digest of `layout.MarketingWithScript(…, "/static/js/landing.js")`) — `layout.*` does
  not change, so this stays byte-identical.
- `TestUnbrandedScreens_AreByteIdenticalToTheGolden` — stays byte-identical (the tap screen is drawn,
  not reused).
- `internal/handler/panelsource_test.go` — the `nonPanelTemplates` classification (§3.10).
- `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` (`internal/brand/theme_test.go:955`): its occurrence
  golden lists `.lp .plaque .p-brand{color:#9db3a5;…`. Recolouring that rule (non-token `#9DB3A5`, §5)
  **edits this golden in the same change**, and the rule's selector keeps the word `brand` only if the
  golden still lists it — renaming the class (e.g. `.p-mark`) removes the line from the golden.
- `TestSignup_MountsExactlyTheRoutesTheLandingPageOffers` (`signup_test.go`) ·
  `TestSignupSurface_EverySignInLinkGoesToTheSignInPage` (marketingChrome's header).
- `TestPage_BareVisitIsALanding` and `TestPage_LandingWithoutALinkSaysWhatIsTrue` (`activate_test.go`) —
  these are the **activation** landing, not `/`; listed because the name collides, they must stay green.
- `TestEveryNamedTestExists` (`cmd/tappa/testnames_test.go`) — every test name written in this README and
  in the M14 cards exists. A test the port adds is **described**, not named, until it exists.
- `web/templates/pages/landingview_test.go` holds nothing on the landing (legal only).

**Deliberate updates in the port** (each in the card that causes it, intent kept, nothing loosened):
`Step.Icon` emoji → an icon key, and `TestLandingSteps_EveryStepIsRenderedInOrder`'s icon assertion
becomes a per-step icon-key check with a control (M14-05) · `tapring`/`printout` move inside
`no-preference` and `TestLanding_ReducedMotionLeavesTheDocketVisible` is extended to the landing docket
with a control (M14-04) · the panel-showcase values get Facts and a
rendered-and-claimed test (M14-08) · the rotation gets three targets and a test that every id landing.js
writes exists exactly once (M14-04; today **no** test pins the rotation's targets) · the brand golden
changes if `.p-brand` is recoloured (M14-01) · marketingChrome gains the ring (M14-02).

---

## 5. Token map (`.lp` block)

The `.lp` block declares the palette as custom properties (`css:1763`): `--ink #152219`,
`--green #1F5C41`, `--green-lite #E1EDE6`, `--porcelain #EDF0EA`, `--paper #FFFDF4`,
`--saffron #D98E2B`, `--saffron-lite #F7EBD6`, `--tomato #BE3D2A`, `--line #C9D2C8`, and `--mint
#8FD3AF` (allowed: plaque wave, ink-section accents). Those stay. Everything else is retired and
expressed as a token plus opacity (`rgb(21 34 25 / .8)` etc. — plain CSS, the block is not Tailwind).
Board values in brackets. **18 hex values and 5 off-palette `rgba()` values** (white and the two dark
shadow inks are not palette colours).

| Retire | Used at (today) | Replace with |
|---|---|---|
| `--ink-soft #3D4F44` | body copy, nav links | ink at .80 (board `rgba(21,34,25,.8)`) / .85 for long copy |
| `--green-deep #17462F` | green button hover | ink (the marketingChrome button's `hover:bg-ink`) |
| `#F4F6F1` | `.strip` ground | paper at .55 |
| `#9AA79C` | `.heroCreed s` | ink at .70, strike stays tomato |
| `#D98B7B` | `.lp-ledger .no` (device column on ink) | paper at .62; the ✗ mark tomato-on-ink only as a mark |
| `#9DB3A5` | `.plaque .p-brand`, `.ledgerWrap .secSub`, `.ledgerNote`, ledger `th` | paper at .62 (on ink). **`.p-brand` edits the brand golden** (§4) |
| `#B9C8BC` | `.lp-ledger td:first-child` | paper at .82 (board) |
| `#33473B` · `#24352B` | dashed rule on the plaque; ledger row rule | paper at .18 / .10 (board) |
| `#E2DFC9` | `.link` card border, docket border | line |
| `#C9C4A8` · `#4B5C50` | `.d-rows` rule and text | dashed line · ink at .80 |
| `#CFE6D8` | `.footCta p` (on green) | paper at .82 |
| `#6FA88B` | `.p-ring`, `.tapPulse` | mint at .75 (board `rgba(143,211,175,.75)`) |
| `#7A9484` | `.p-loc` | paper at .62 |
| `#F2F5EF` | `.plaque` text | paper |
| `#fff` | `::selection`, `.dot::after`, button text | paper |
| `#E9EFE9` | `.lp .ledgerWrap` text (`css:~1901`, the ink comparison panel) | paper at .92 (or paper) |
| `rgba(255,255,255,.7)` | `.footCta .lp-btn-onGreen` border (`css:~1972`) | paper at .70 |
| `rgba(255,255,255,.14)` | `.footCta .ringBg` border (`css:~1975`) | paper at .15 |
| `rgba(15,25,18,.35)` | plaque and demo dialog shadow (`css:~1851`, `~2027`) | ink at .35 |
| `rgba(15,25,18,.62)` | `.demoDialog::backdrop` (`css:~2030`) | ink at .62 |
| `rgba(20,30,22,.18)` | `.lp-docket` shadow (`css:~1866`) | ink at .20 (or .18) |

Contrast rules from the panel carry over: text at least ink/.70 on paper (6.05:1); "STEP n" is ink/.70,
**not saffron** (saffron text on paper is 2.62:1); ink/.50 is for borders only. On ink grounds, paper
at .62 is the floor for small text (board). Status is never colour alone (✓/✗ marks are
`aria-hidden`, the words carry it).

**Radius (D2):** docket, stamp, plaque ticket → sharp (0, at most 2 px); cards, dialogs, miniature
windows → 14–18 px; buttons → 12 px (the green rounded-12 of the redesign); chips → full.

**Mono weights to remove** (500/600 → 400 or 700): `.heroCreed` (400), `.eyebrow` (`css` ~`:1840`,
600 → 700 or 400 per board), step `.n`, link `.proof`, fit `.k`, the `d-*` docket rules, ledger `th`,
strip `b`. Space Grotesk is variable; its 500 stays legal.

---

## 6. Screen map — section by section

Legend: **EXISTS** unchanged in substance, only the look moves · **CHANGED** says what changes ·
**NEW** not on the page today · **NEW CLAIM** new copy → a `landingview.go` value + Fact + test.

Section order keeps the reference (LP-0): header → hero → strip → how → why switch → security →
**the panel (NEW)** → fits → pricing → FAQ → CTA → footer, plus the dialog.

### 6.1 Header (`landingNav`, `landing.templ:110`)

| Element | Source | Status |
|---|---|---|
| Lockup: ring mark + "taptime" + PUNCHLESS | `landing.templ:113` (`.wordmark .dot`) | CHANGED: PUNCHLESS mono tag (11 px, ink/.70) beside the wordmark; ring 28 + taptime 24, centre-aligned; phone: ring 26 + taptime/PUNCHLESS stacked. Link target **`/`** (today `href="#"`) — the same as marketingChrome. One lockup on every marketing page (§7). |
| How it works · Security · Pricing | `LandingNav` (`view:383`) | EXISTS (`#how`, `#security`, `#pricing`); hidden below 720 px |
| Sign in | `LandingView.SignInHref` (`landing.templ:118`) | CHANGED: visible at **every** width (LP-2.1). Hide only the three anchors. |
| Start free | `LandingView.SignupHref` (`:119`) | EXISTS; the redesign's green rounded-12 button |
| Sticky, porcelain .88 + blur, hairline | `css:1804` (`.lp nav`, `rgba(237,240,234,.88)`) | EXISTS (the board's porcelain/.94 is a mock-up value; keep the code's .88 or move to .94 as one decision, either is the porcelain token) |

### 6.2 Hero (`landingHero` `:145`, `landingScene` `:196`)

| Element | Source | Status |
|---|---|---|
| Eyebrow "Time & attendance for shift teams" | `:149` | EXISTS |
| H1 "Punchless. / Every shift starts with a tap." | `:150` | EXISTS (60 / 58 / 40 px) |
| Lede | `LandingHeroLede` (`view:403`) | EXISTS |
| Creed "No app. No device. No fingerprints. Just tap." | `:152` | CHANGED: Plex Mono **400**; struck words ink/.70 with a tomato strike; "Just tap." 700 ink; each phrase `nowrap`. Readable text stays exactly the slogan (`TestLanding_CarriesTheSlogan`). The 400 weight is M14-01; the per-phrase `nowrap`, the 700 on "Just tap." and the ink/.70 strike colour are **M14-04**. |
| "Start free — 3 months on us" | `:154`, `FreeMonths` | EXISTS |
| "Watch demo" `<a id="demoOpen" href="#how">` + play icon | `:155`, `landingPlayIcon` `:177` | EXISTS (pinned by `marketingdemo_test.go`; keep the existing icon) |
| Foot line | `LandingHeroFoot` (`view:410`) | EXISTS |
| Plaque (TAPTIME, ring, "Tap here to / clock in / out", venue · uid) | `:198–209` | CHANGED: nothing overlaps it (LP-2.2). The venue · uid line **rotates** (`id="pPlaque"`). |
| Ticket (`dName`, `dType`, `dTime`, `dLoc`, "SUN valid ✓", "IP + GPS ✓", APPROVED) | `:210–220`; landing.js `people[]` | CHANGED: hangs from a short ink **ticket rail** (the panel's rail motif) **under** the plaque instead of overlapping it; sharp corners, perforated foot (D2). The stamp moves to the ticket's top row beside the CLOCK IN/OUT tag, so it never covers a row (today `.d-stamp` is absolute over "On site"). Rows wrap instead of colliding; the name has its own line. **The four ids stay.** |
| Employee's phone showing the tap screen | drawing of `pages.Tap` (`tap.templ:55–123`; shell `max-w-md px-4 py-8 gap-6`; unbranded wordmark) | NEW drawing, **no new claim**: status bar, "taptime" + "punchless", a docket ("TAPPING" / "Hello {name}" / venue) and one "Tap" button (64 px, rounded-sm, green, text-xl 400). Drawn at real CSS sizes in a 375-wide screen, then scaled by a fixed class (`scale(.61)` desktop / `.58` tablet / `.384` phone). The name and venue rotate (`id="pName"`, `id="pLoc"`). The button is a `<span>`, not a `<button>` (it is a picture). |
| NFC arcs between plaque column and phone | inline SVG, `stroke="currentColor"` | NEW decoration, `aria-hidden` |
| Scene layout | — | Same structure at every width: [plaque over hanging ticket] · arcs · phone. Tablet: text above. Phone: the rail hangs under the plaque. Flex, **no absolute positioning**, so nothing overlaps at any width. |

**Rotation (R-1).** Each record gains a `plaque` field; the records (landing.js `people[]`) use KF
venues except the KM person:

| Person | Type | Time | Venue (ticket + phone) | Plaque line |
|---|---|---|---|---|
| Maria Vella | CLOCK IN | 09:58 | KF St Julians | KF ST JULIANS · 91AC-7E55 |
| Kevin Grech | CLOCK IN | 09:52 | KF St Julians | KF ST JULIANS · 91AC-7E55 |
| Anna Farrugia | CLOCK IN | 04:02 | KM Plant (LP-6: was "KM Facility") | KM PLANT · 2F04-1B9A |
| Luke Borg | CLOCK OUT | 02:04 | Rusty Bar (a KF venue in `seed.sql`) | RUSTY BAR · 6D31-C8E2 |

The two new uids are invented, like 91AC-7E55 (a drawing, not a record — the existing comment at
`landing.templ:184–195` says so and is extended). The first record is the server-rendered one. The
phone's accessible name must not go stale: keep its `role="img"` label generic ("The employee's own phone
showing the tap screen") — do not put the rotating name in `aria-label`.
**Animation — today (measured at `945b6b9`).** `tapring` and `printout` are declared at the **top level**
of the `.lp` block (`.lp .scene.play .tapPulse { animation: tapring … }` `css:~1862`,
`.lp .scene.play .lp-docket { animation: printout … }` `css:~1867`). `.lp .lp-docket` **starts clipped**
(`clip-path: inset(0 0 100% 0)`, `css:~1866`), and only the `prefers-reduced-motion: reduce` block at
`css:~1987–1990` undoes it (`animation: none; clip-path: none`). That is the hazard the signup docket was
fixed for: drop or rename the reduce block and a reader who asked for less motion sees no ticket.
`TestLanding_ReducedMotionLeavesTheDocketVisible` does **not** guard this — it scans only signup's
`docket-print` rule.
**Animation — the port (M14-04).** The boards are static. Keep the tap pulse and the print-out loop, but
move **both** animations (and the clipped starting state) **inside** `@media (prefers-reduced-motion:
no-preference)`; outside it the docket is fully visible by default and the reduce block is no longer
needed. Extend `TestLanding_ReducedMotionLeavesTheDocketVisible` (or add a described test) to cover
`printout`, `tapring` and the `.lp-docket` clip, with a control that puts today's top-level rule back and
goes red. The print-out reveals the ticket **downward from the rail**, not over the plaque.

### 6.3 Pilot strip (`landingStrip` `:231`)

"Piloting with teams in Malta · KEBAB FACTORY LTD. — 9 locations · KEBAB MANUFACTURING CO. — 5
departments" — EXISTS. Ground paper/.55 (today `#F4F6F1`, retired in M14-01). Phone: centred and stacked
(**M14-09** — the strip has no card of its own and rides with the fits/pricing restyle).

### 6.4 How it works (`#how`, `landingHow` `:242`)

| Element | Source | Status |
|---|---|---|
| Eyebrow / H2 / lede | `:245–247`, `LandingSetupLede` (`view:419`) | EXISTS |
| Three cards | `LandingSteps` (`view:484`; `Step.Icon` `view:474`) | CHANGED: the emoji (🏷️ ✉️ 📶) become line icons (§8.3: plaque, envelope, phone with waves) in a 44 px green-lite tile. Port: replace `Icon string` with an icon key and draw the SVG in templ (a `switch` on the key). **A test does pin the emoji:** `TestLandingSteps_EveryStepIsRenderedInOrder` (`internal/handler/marketing_claims_test.go:590`) requires `s.Icon` to appear in the rendered text. Change that assertion **in the same commit**, intent kept — each step renders **its own** icon (e.g. a per-key marker such as `data-icon="plaque"` on the step's SVG, asserted per step in order) — with a control (swap two keys, or drop one SVG, and watch it go red). "STEP n" ink/.70. Tablet: the cards become rows. |

### 6.5 Why switch — comparison (`landingLedger` `:279`)

| Element | Source | Status |
|---|---|---|
| Eyebrow / H2 / sub | `:283–285` | EXISTS |
| Table | `LandingComparison` (`view:558`) | CHANGED: **both** columns at every width (today `css` hides column 2 below 640 px). Desktop/tablet: 3 columns, the ✗/✓ mark in its own span so wrapped lines align under the text. Phone: each row stacks aspect / ✗ device / ✓ Taptime under a two-chip legend. DOM text order stays aspect → device → Taptime (what `TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact` and `TestLanding_ComparesMechanismsAndDeclaresNoWinner` read). Device text paper/.62. |
| Footnote | `LandingComparisonNote` (`view:607`) | EXISTS |

### 6.6 Security (`#security`, `landingSecurity` `:311`)

| Element | Source | Status |
|---|---|---|
| Eyebrow / H2 / lede | `:314–316`, `LandingSecurityLede` (`view:428`) | EXISTS |
| Four cards | `LandingEvidence` (`view:634`) | CHANGED: panel dockets (D2), green proof eyebrow; grid 4 / 2 / 1 |
| Privacy note | `LandingPrivacyNote` (`view:436`) | CHANGED: spans the full grid width, gains the shield icon (§8.3) |

### 6.7 The panel (`#panel`) — NEW section, after Security

Placed after Security because the four proofs end in "queued for manager review"; the reader's next
question is where those records land, and the panel answers it. Every line is **NEW CLAIM**: in the port
each becomes a `landingview.go` value with a Fact (or Anchor) that a test derives from the code, with a
mutation control that removes the code fact and watches the test go red, and a check that the value is
rendered (`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` then covers it).

| # | Element | Copy (verbatim) | Fact it rests on (derivation) |
|---|---|---|---|
| P0 | Eyebrow | "The panel" | furniture (no claim) |
| P1 | H2 | **"Every clock-in lands on The Pass."** (was "Every tap…" — false: no-session, activation and other-tenant taps write no record) | **New Fact** (proposed `FactEveryDecidedTapIsRecorded`): `tap.Decide` maps every effect except `redirect` to a recorded verdict (allow→ok, review→flag, deny→reject, ignore→ignored — `internal/domain/tap/decide.go:44–50`); the checkin path writes that verdict to `transactions`; The Pass lists `transactions` for the day (`ListPanelTransactions`, `db/queries/transactions.sql:423`). Derivation: the effect→verdict map has exactly one non-recording branch (`redirect`) + the Pass route is mounted. A "clock-in" is a tap by a recognised person — exactly the set never redirected. |
| P2 | Sub | "Managers work from the dashboard: the day's taps on The Pass, the ones the rules could not judge on The Spike, and the week's hours on The Roll." | the three panel routes are mounted (Pass, Spike, Roll) + `FactFlaggedQueueDecidedByAManager` + `FactReportPerPersonAndVenue` (both exist in `view`) |
| P3 | Caption 1 | "The Pass" — "The day's taps as dockets, newest first, each stamped with its verdict." | the transactions list renders `DocketView` with verdict stamps; order `ORDER BY t.occurred_at DESC, t.id DESC` (`transactions.sql`, in `ListPanelTransactions`) |
| P4 | Caption 2 | "The Spike" — "Taps the rules could not judge on evidence alone wait here for a manager to approve or reject." | `base:no-evidence-review` = `EffectReview` + `POST reviewHref` (= `FactFlaggedQueueDecidedByAManager`) |
| P5 | Caption 3 | "The Roll" — "Hours per person for the week, with the CSV for your payroll." | `FactHoursExportAsCSV` (`route:GET reportsCSVHref`) + the weekly report route |
| P6 | Footnote | "Drawn for this page — sample names and times." | honesty line (no claim; the drawing's own disclaimer) |

The screen names (The Pass, The Spike, The Roll, People, Venues & plaques, Odd shapes, Rules) are the
panel's shipped tab labels (user decision 2026-10-20) — `TestLanding_ShowsNoUserFacingInternalName`
must still pass (they are user-facing names, not internal ones).

**Miniatures** — HTML/CSS only, no `<img>`. Each is a mini panel window: an ink rail with **7 marks in
`Main.dc.html`'s nav order** (The Pass, The Spike, People, Venues & plaques, The Roll, Odd shapes, Rules),
active pill at index 0 / 1 / 4. Each carries `role="img"` + an accessible name ("Sample screen: The
Pass" …); rails and decorations `aria-hidden`.
- **The Pass:** a ticket rail of 4 dockets: venue, stamp, name, IN/OUT + time, TRUST and evidence. The
  evidence strings are the panel's own (`Main.dc.html`): "IP yes · GPS n/a" (70), "IP n/a · GPS yes"
  (50), "IP no · GPS n/a" (20, FLAGGED). Phone: 2 dockets.
- **The Spike:** "Waiting for review: 3"; a queue of 3, the first selected; the selected FLAGGED docket
  labelled with the full 14-hex uid ("KF MSIDA · PLAQUE 04A1C33C2B7E10"); tiles **Physical tap: yes /
  Network: no / Location: no** (`Spike.dc.html` wording); Approve / Reject (drawn, not buttons).
- **The Roll:** "Hours per person · week 42"; Person · MON–SUN · Total in the code's "8h 02m" format; one
  "open" cell; a CSV mark. An empty day "—" is ink/.70. Phone: per-person rows with 7 day squares.

**Sample data — invented, one tenant (Kebab Factory), agrees with the hero:**
- Pass: Maria Vella IN 09:58 and Kevin Grech IN 09:52 at KF ST JULIANS (as the hero tickets); Grace
  Attard 09:41 at KF MSIDA (FLAGGED); Luke Borg OUT 02:04 at RUSTY BAR.
- Roll: Maria Vella (Line Cook · KF St Julians), Kevin Grech (Kitchen Porter · KF St Julians), Grace
  Attard (Line Cook · KF Msida), Luke Borg (Bar · Rusty Bar). No KM person (two tenants cannot share a
  Roll).
- No figure that trips `TestLanding_MakesNoUnmeasuredNumericClaim` (no "100", "99", "%") — so no TRUST
  100. The sample data is **furniture of the drawing**, not a claim: keep it in the template (or one
  landing-only Go value marked as sample), never in a `Line`/`Fact` value.

### 6.8 Built for both shapes (`landingFits` `:335`)

`LandingAudiences` (`view:721`) — EXISTS. Kicker green; chip rounded, green on green-lite. 2 columns /
1 column.

### 6.9 Pricing (`#pricing`, `landingPricing` `:363`)

| Element | Source | Status |
|---|---|---|
| H2 / lede | `LandingPricingHeading` (`view:447`), `LandingPricingLede` (`view:453`) | EXISTS |
| "€1.50 / employee / month" | `PricePerEmployeeMonth` (migration 00016) | EXISTS (display face) |
| "A 60-person chain runs on €90/month." | `ExampleHeadcount` / `ExampleMonthly` (`marketing.go`, `exampleTeamMonthly`); rendered only when non-empty | EXISTS |
| Founding offer line | `:375`, `FreeMonths` | CHANGED look: ink on saffron-lite, saffron border |
| Includes list | `LandingPricingIncludes` (`view:782`) | CHANGED: the ✓ pseudo-element becomes the check icon (§8.3) |

### 6.10 FAQ (`landingFAQ` `:388`)

| Element | Source | Status |
|---|---|---|
| Eyebrow / H2 | `:391–392` | EXISTS |
| Five `<details>` | `LandingFAQ` (`view:835`) | CHANGED layout: two columns on desktop (heading sticky left, list right; LP-3), one column below. "+" turns to "×" when open, CSS only. The board shows item 1 open; the product renders all closed. |
| Q4 "How do we migrate from our current device?" | `LandingFAQ[3]` | **CHANGED by the FAQ-API fix (R-2, separate branch).** New answer: "Add your people from the dashboard and hand out invite links — one profile at a time; there is no file import today. If your HR system already holds the list, the integration API can write your people and locations once Taptime enables it for you. The old box is yours to unplug whenever you're ready — Taptime doesn't talk to it." |
| Q5 "Where does the data go?" | `LandingFAQ[4]` | **CHANGED by the FAQ-API fix (R-2).** New answer: "Into your dashboard: hours per person and per site, and the monthly headcount you're billed on — both as CSV exports for your payroll. Once Taptime enables the integration API for you, your HR or payroll system can also read every punch from a feed or receive it by webhook. Taptime is the source of truth for in/out times — it doesn't try to replace your payroll." |
| Q1–Q3 | `LandingFAQ[0..2]` | EXISTS (ASCII apostrophes, as in the Go values) |

### 6.11 CTA (`#pilot`, `landingCTA` `:416`)

"Go punchless." / "Start yourself today — first 3 months free." / "Start free — 3 months" — EXISTS. One
button; "Book a pilot" stays absent.

### 6.12 Footer (`landingFooter` `:439`)

| Element | Source | Status |
|---|---|---|
| "© 2026 Taptime · Malta" | `:442` | CHANGED: the flag emoji removed (LP-2.4); a small ring mark (22) before it |
| "tap in · tap out · that's it" | `:443` | EXISTS |
| Privacy · Terms · Company details · Cookies · API docs | `LegalPages` (`view:925`) + `APIGuidePath`; `<nav class="wrap footLegal" aria-label="Legal and developers">` (`:445`) | EXISTS; the Legal board's link style, 44 px targets |

### 6.13 Demo dialog (`landingDemoDialog` `:481`; boards `LandingDemo`, `M-LandingDemo`)

| Element | Source | Status |
|---|---|---|
| `<dialog id="demoDialog">`, title, close (autofocus, 44×44) | `:481–501` | EXISTS. The board draws the autofocus ring (saffron, `.lp :focus-visible`). |
| `soon` state copy + CTA | `:485–538` | EXISTS (verbatim). Phone: the CTA full width; the dialog centred (`margin: auto`). |
| `video` state: "The Taptime demo" + native `<video controls preload="metadata" playsinline>` | `:503–519` | EXISTS. The board draws a poster and the browser's own controls (not product UI). |
| Backdrop | `css` `::backdrop` | ink at .62 (optional change) |

---

## 7. The shared marketing header (ring mark on Signup / Legal / API guide)

The boards draw **one lockup on every marketing page**: ring mark 28 (a tappa-green disc with a 2 px
paper ring inset 6 px, opacity .92, `aria-hidden`) + "taptime" 24 + PUNCHLESS 11 mono, centre-aligned,
the whole lockup a ≥ 44 px link to `/`; phone: ring 26 + taptime/PUNCHLESS stacked. In the fix round the
ring was added to the `Signup`, `T-Signup`, `M-Signup`, `Legal`, `T-Legal`, `M-Legal` board headers, which
had none.

**Port (M14-02):**
- `marketingChromeWith` (`landing.templ:635`) — the lockup link (`<a href="/" class="flex min-h-11
  items-baseline gap-2">`) gains the ring as a decorative `<span aria-hidden="true">` before "taptime"
  (Tailwind utilities written literally; no new `.lp` class, the chrome is not under `.lp`), and
  `items-baseline` becomes `items-center`. The landing's own `landingNav` gets the same lockup in `.lp`
  classes (§6.1). The two must read as one header.
- **The API guide's bytes change on purpose.** `marketingChrome`'s comment (`landing.templ:580–585`)
  records that the zero options render the guide's chrome byte for byte as before the redesign (M12-46
  measured the guide's two pages, with and without an API host). There is **no golden file** for
  marketingChrome; the invariant is that comment plus the tests that read the chrome. The ring is a
  deliberate change of those bytes for signup, legal and the guide **together**: rewrite the comment
  with the new measurement (each page's body unchanged, only the lockup's bytes added — written in the
  commit message, the `0bb5085` method), and keep green `TestAPIGuidePages_AreTheGuideInTheMarketingChrome`,
  `TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel`, `TestAPIGuidePages_FitAPhone`,
  `TestSignupSurface_EverySignInLinkGoesToTheSignInPage`,
  `TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce`.
- `layout.Marketing` / `layout.MarketingWithScript` do **not** change
  (`TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` holds their digests) — the ring lives in
  `marketingChromeWith`, a `pages` component.
- The footer family: marketingChrome's footer is already the Legal board's; the landing footer moves to
  the same link style (§6.12) but keeps its own two lines.

---

## 8. Port notes

### 8.1 Markup

- **One `<nav>` first** with the three anchors + Sign in + Start free (§3.7); phone hides the anchors by
  CSS. No second nav before it. The footer's `<nav aria-label="Legal and developers">` stays where it is.
- **Phone greeting is not a heading** (§3.5). Draw a landing-only copy whose text matches `tap.templ`
  ("Tapping", "Hello {name}", the venue, "Tap").
- **`role="img"` with an accessible name** on the phone and on each miniature; decorative arcs, rails and
  marks `aria-hidden`. Keep the phone's label generic (§6.2).
- **Fixed classes, never inline style:** the phone frame, its screen size and the 375 → N scale per
  breakpoint; docket tilts (`nth-child`); Roll cell states (worked / none / open); day squares.
- New ids: `pName`, `pLoc`, `pPlaque` (hero), `panel` (the section). Each id once on the page.
- If a component moves to a new `.templ` file, classify it (§3.10).

### 8.2 CSS / JS / CSP

- **CSP:** unchanged (§3.1).
- **JS:** `landing.js` gets three more `textContent` writes per tick (R-1) and a `plaque` field per
  record; nothing else. Its header comment ("IT TOUCHES NOTHING BUT FOUR TEXT NODES", "carried over …
  UNCHANGED") is rewritten in the same change: **seven** text nodes now, and the extra three are the
  user's R-1 decision (2026-10-21), not the reference. The demo half of the file is untouched (its
  strings are pinned).
- **Fonts:** remove the mono 500/600 from the `.lp` block (§5).
- **Colours:** retire the non-token values (§5).
- **One stylesheet.** `app.css` serves the landing and the panel; the miniatures do **not** reuse panel
  component classes (`.docket`, `.rail-item`, `.stamp` …) — their AA grounds and the panel's goldens are
  pinned for the panel. Scope every miniature rule under `.lp`. (An earlier note said "panel CSS is not
  loaded on `/`"; it is one file — what matters is that the landing does not depend on panel classes.)

### 8.3 Icons (inline SVG, `viewBox="0 0 24 24"`, `fill="none"`, `stroke="currentColor"`, `stroke-width="1.8"`, `stroke-linecap="round"`, `stroke-linejoin="round"`, `aria-hidden="true"`)

- plaque (step 1) `<rect x="5" y="2.5" width="14" height="19" rx="2.5"/><path d="M8.8 10.2a4.6 4.6 0 0 1 6.4 0"/><path d="M10.4 12.4a2.2 2.2 0 0 1 3.2 0"/><circle cx="12" cy="14.6" r=".5" fill="currentColor"/><path d="M9 18.2h6"/>`
- envelope (step 2) `<rect x="3" y="5" width="18" height="14" rx="2.5"/><path d="M3.8 6.6 12 12.8l8.2-6.2"/>`
- phone with waves (step 3) `<rect x="3.5" y="2.5" width="10" height="19" rx="2.2"/><path d="M7.5 18.5h2"/><path d="M17 9a4.5 4.5 0 0 1 0 6"/><path d="M19.8 6.6a8 8 0 0 1 0 10.8"/>`
- shield (privacy note) `<path d="M12 3l7 3v5.2c0 4.4-3 8-7 9.8-4-1.8-7-5.4-7-9.8V6z"/><path d="M9 12.2l2.1 2.1L15.2 10"/>`
- check (pricing includes) `<path d="M5 12.5l4.2 4.2L19 7"/>`
- FAQ "+" (stroke 2.2) `<path d="M12 5v14M5 12h14"/>`, rotated 45° when `[open]` (CSS only)
- The existing play icon (`landingPlayIcon`, stroke 1.6) stays as it is.

### 8.4 Sizes (from the boards)

| | plaque w | ring Ø | phone w | screen h | docket w | phone scale |
|---|---|---|---|---|---|---|
| desktop | 226 | 84 | 252 | 486 | 226 | .61 |
| tablet | 214 | 80 | 240 | 462 | 214 | .58 |
| phone | 176 | 64 | 158 | 330 | 176 | .384 |

### 8.5 Breakpoints (`.lp` media queries)

900: hero stacks · 860: steps become rows · 720: anchors hide, **Sign in stays** · 640: comparison
switches to stacked rows and **never hides a column** · 540 / 960: proof grid 1 / 2 / 4 · FAQ two
columns on desktop only (pick the breakpoint with the hero's 900 or the proof grid's 960; the board
draws two at 1440, one at 834).

---

## 9. Board notes (D1 corrections the port applies)

- Board links are `href="#"`; the port uses `/` for the lockup and the real routes elsewhere.
- Board sample data (names, uids, times, trust, hours) is drawing furniture (§6.7), never product fact.
- Board inline styles, Google-Fonts `<helmet>` and the `text/x-dc` script are mock-up machinery (panel
  README §1).
- The board draws FAQ item 1 open and the dialog's focus ring — states, not defaults.
- The FAQ Q4/Q5 wording on the boards is R-2's; the code carries it once `main` (with `0e7c158`) is
  merged into the work branch (M14-10).
- `canvas.json` is kept in the repo's compact single-line JSON (as it always was); that is intended, not a
  formatting slip.

---

# Part II — LP-6: more public pages and more landing (user, 2026-10-21)

Part II extends the handoff for the user's second request: *"dahada landingi cogalt . daha baska pageler
olsun"* ("make the landing bigger, add more pages"). The user chose **all** of the offered pages. Both
builders' boards passed an independent audit (two rounds, plus a one-line S36 wording fix). The
claim-level detail is in the two inventories copied next to this README —
[`lp6-inventory-a.md`](lp6-inventory-a.md) and [`lp6-inventory-b.md`](lp6-inventory-b.md). This part
gives the decisions, the routes, the screen map and the NEW CLAIM groups with their Facts. It links to the
inventory rows rather than repeating all of them.

## 10. LP-6 decisions — binding

- **LP-6.1 — Five new public pages**, each at three widths:
  - **`/product`** — a product tour: the employee's tap (tap screen and result screen as `tap.templ` /
    `result.templ` draw them), then the manager panel screen by screen (The Pass, The Spike, The Roll,
    People, Venues & plaques, Rules — real names, real behaviour, D1);
  - **`/security`** — the four proofs in depth, what is never collected, records never lost, immutability,
    tenant isolation in plain words, EU hosting (**gated**, §16.1), GDPR;
  - **`/pricing`** — price per person, the founding offer, a team-size table **computed server-side**,
    what is included, a billing FAQ grounded in the billing code, the "API enabled by Taptime" note;
  - **`/for/restaurants`** and **`/for/production`** — industry pages built on the existing
    `LandingAudiences` facts. The pilot strip may appear. No testimonials, quotes, logos or invented metrics.
- **LP-6.2 — Three new landing sections:** "For your team" (`#team`), "A shift, start to finish"
  (`#shift`), "Integrations" (`#integrations`), plus links from the landing to the new pages.
- **LP-6.3 — Shared navigation on every public page** (landing, the five pages, signup, legal, API guide):
  - **header (desktop/tablet):** ring lockup · **Product · Security · Pricing** (page links, not anchors) ·
    Sign in · Start free; the current page's link carries `aria-current="page"` (ink, 700, green 2 px
    underline);
  - **phone header:** lockup · Sign in · Start free · a native **`<details>` "Menu"** (no JS). The summary
    reads "Menu" when closed and "Close" when open (two spans swapped by `details[open]` CSS). The open
    panel has the label "Pages" and six links (Product tour, Security, Pricing, For restaurants & bars, For
    production & facilities, API docs) in 48 px rows. It sits absolutely under the sticky header, so it
    overlays the page without reflow;
  - **footer:** a brand block ("© 2026 Taptime · Malta", "tap in · tap out · that's it", and the
    **marketingChrome claim paragraph, kept verbatim**: the "Taptime" label + "Punchless time and
    attendance. Taptime collects no biometric data of any kind: no fingerprints, no face, no voice. A
    phone's position is read only at the moment somebody presses the button."). Then a **Product** column
    (Product tour, Security, Pricing, For restaurants & bars, For production & facilities, API docs) and a
    **Legal** column (Privacy, Terms, Company details, Cookies). "API docs" moves from the legal row into
    Product.
  - The industry pages mark nothing as current, because the header has no industry item.
- **Same rules as LP-1…LP-5:** D1; existing claims verbatim; every new sentence is a NEW CLAIM with a Fact;
  palette; D2; Plex Mono 400/700; no emoji; no `img-src`; no inline style; **no new JS beyond landing.js**.
- **Sample data is unified** across all 26 boards:
  - one tenant per drawing;
  - venues, hours and networks from `test/fixtures/seed.sql`;
  - the week-42 Roll rows are identical on every page;
  - Maria Vella is never at two venues at once;
  - **the KM venue is "KM Plant" everywhere** (coordinator decision, from the seed). The landing
    rotation's Anna Farrugia record changes "KM Facility" → **"KM Plant"** and its plaque line to **"KM
    PLANT · 2F04-1B9A"** (§6.2 table updated; part of M14-04's `landing.js` edit);
  - department names follow the seed ("KM Pastry & Bakery", "KM Meat Production", …).
- **Drawn phones** use an **ink** wordmark when there is no logo (`base.templ:174-176`). At 390 px the
  two-phone figures are stacked, each at about 0.62 scale.

## 11. Routes and serving

| Path | Template (new) | Current nav item | Board |
|---|---|---|---|
| `/product` | product page | Product | `Product` ×3 |
| `/pricing` | pricing page | Pricing | `Pricing` ×3 |
| `/security` | security page | Security | `Security` ×3 |
| `/for/restaurants` | industry page | — | `ForRestaurants` ×3 |
| `/for/production` | industry page | — | `ForProduction` ×3 |

- **Served by `internal/handler/marketing.go`**, mounted in `Marketing.Mount` beside `/legal/*` and
  rendered with `m.render` → **`marketingCSP`** (`default-src 'none'; style-src 'self'; font-src 'self'; …`).
  That means **no `script-src`** (these pages carry no script at all; only `/` has `landing.js`), **no
  `img-src`** (inline SVG only) and **`Cache-Control: public, max-age=300`** (`marketingCacheControl`),
  exactly like `/legal`. Same bytes for every visitor, no session, no panel headers. They are served on the
  product host only, like the API guide.
- Each page is a `pages` templ component in a **new `.templ` file**, wrapped in `layout.Marketing(…,
  layout.RobotsPublic)` — **not** `MarketingWithScript`. Each new file goes into `nonPanelTemplates` with
  the reason "marketing" (§3.10). The paths are **route constants** in the handler, the way `signupPath`
  and `APIGuidePath` are, never typed into templates. A shared `PublicPages` list (path, label, short
  label) feeds the header, the phone Menu and the footer, so the three can never disagree.
- The pages reuse the landing's look. Either share the `.lp` block — wrap each page in `<div class="lp">`,
  which is simplest because every miniature, docket and phone already lives there — or move the shared
  rules into a public-site scope used by all six. Pick one in M14-14 and record it. Do not reuse panel
  component classes (§8.2).
- **Pinning tests (new; the builder names them, until then they are described):**
  - a route test: each path answers 200 on GET and HEAD on the product host, 404 elsewhere, and is listed
    in the shared nav source;
  - a CSP test: each page carries exactly `marketingCSP` — no `script-src`, no `img-src`, no media — and
    loads no `<script>`;
  - `TestTemplates_CarryNoInlineStyle` (already covers every template), plus `TestLanding_UsesOnlyPaletteTokens`'s
    hex and ` style=` checks run over every new page;
  - an every-public-page-links test: every public page (landing, five pages, signup steps, legal ×4, API
    guide ×2) carries the same header links (with `aria-current` on at most one, the right one), the Menu
    with its six links, and the footer's Product and Legal columns. Control: drop one link from one page and
    the test goes red;
  - `TestMarketing_EveryInternalLinkResolves`, `TestMarketing_ReachesNoThirdParty`,
    `TestMarketing_TheSameBytesForEveryVisitor`, `TestMarketing_IsCacheableAndCarriesNoPanelHeaders`,
    `TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders` and `TestLanding_MakesNoUnmeasuredNumericClaim`
    extended to the new paths (whichever iterate a path list get the five added);
  - `TestLandingDemo_NoOtherScreenOnThePublicSurfaceCarriesTheDialog` covers the new pages too.

## 12. Shared public chrome — screen map

| Element | Status | Port note |
|---|---|---|
| Header lockup (ring 28 + taptime 24 + PUNCHLESS 11; phone ring 26, stacked) | CHANGED (§7) | unchanged by LP-6 |
| Header links Product · Security · Pricing | **CHANGED** — pages, not `#how/#security/#pricing` anchors | `LandingNav` becomes the shared public nav. **`TestLandingNav_EveryLinkPointsAtASectionOnThePage` is rewritten deliberately**: it reads the first `<nav>` and today requires in-page anchors. The new intent is that every bar link points at a **mounted public page**, the current page is marked, and the bar still carries signup and sign-in. Its control: a link to an unmounted path goes red. Keep ONE first `<nav>` (§3.7). |
| Sign in · Start free | EXISTS (visible at every width, M14-03) | — |
| Phone `<details class="menu">` "Menu"/"Close", panel "Pages" + 6 links | **NEW** | no JS. The `<details>` sits **after** the first `<nav>` in the DOM, or inside it. The 48 px rows are touch targets. The summary is a real `<summary>` with a visible label. The board prop `menu = closed \| open` only toggles the drawing. |
| Footer brand block + claim paragraph | CHANGED | the claim paragraph is marketingChrome's, verbatim |
| Footer Product column (6 links) | **NEW** | "API docs" moves here; `TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel` must still find it on the landing and in marketingChrome |
| Footer Legal column (4 links) | CHANGED (from a row) | `TestMarketing_MountsEveryLegalDocumentAndLinksToIt`, `TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce` |

**`marketingChromeWith` gains the LP-6 header and footer**, so signup, legal and the API guide render the
same chrome as the landing. This is a **deliberate change of the guide's bytes** (the M12-46 note at
`landing.templ:580–585` is rewritten with the new measurement, as in §7). Tests that read that chrome stay
green, with any byte-level assertion updated in the same commit and its intent kept:
`TestAPIGuidePages_AreTheGuideInTheMarketingChrome`, `TestAPIGuidePages_FitAPhone`,
`TestSignupSurface_EverySignInLinkGoesToTheSignInPage`, `TestSignup_MountsExactlyTheRoutesTheLandingPageOffers`.
`layout.*` does not change (`TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` stays byte-identical).
The landing keeps its own `.lp` markup for the same chrome. The two must render the same links in the same
order, and the every-public-page-links test is what holds them together.

## 13. Landing — new order and sections (inventory A §1)

header → hero → strip → how → why switch → **For your team (NEW, `#team`)** → security → the panel → **A
shift, start to finish (NEW, `#shift`)** → fits → **Integrations (NEW, `#integrations`)** → pricing → FAQ
→ CTA → footer.

Links to the new pages are furniture, not claims:
- "How each proof works →" (end of Security → `/security`);
- "Take the product tour →" (beside the panel footnote → `/product`);
- "For restaurants & bars →" / "For production & facilities →" (one per fits card → `/for/…`;
  `LandingAudiences` gains `Href`);
- "Pricing in detail →" (under the price card → `/pricing`);
- "Read the API docs →" (integrations → `APIGuidePath`).

| Section | Status | What is drawn | Claims |
|---|---|---|---|
| `#team` | NEW | two phones at real CSS sizes: the tap screen (unchanged) and the **result of a flagged tap** (`result.templ` flag branch; Grace Attard · KF Msida · 09:41 · Trust 20). Flagged rather than approved, because the approved result prints a business-type line that carries an emoji for both pilot trades (LP-2.4). | T2–T9 (+ honesty line T10) |
| `#shift` | NEW | six dockets on an ink rail (desktop 1×6, tablet 2×3, phone a vertical list); step 04 FLAGGED; actor chips are furniture | S2–S9 |
| `#integrations` | NEW | three cells (CSV · integration API · webhooks) + a note bar; the mono chips are **real identifiers** (`Download CSV`, `PUT /v1/employees/{ref}`, `PUT /v1/locations/{ref}`, `GET /v1/punches`, `X-Taptime-Signature: sha256=…`), pinned to the route constants and `internal/webhook/signing.go` | I2–I7 |
| fits cards | CHANGED | one link per card | — |

## 14. `/product` — Product tour (inventory A §2)

- **Structure:** header (Product current) → hero (eyebrow, H1, lede, "Start free — 3 months on us" +
  "See pricing"; an "On this page" index with anchors `#tap #pass #spike #roll #people #venues #rules`) →
  seven tour sections → honesty footnote → Integrations (the same values as `#integrations`) → CTA → footer.
- **Each tour section:** a numbered node, a kicker (furniture), an H2 that is the real screen name, one
  line, three or four check bullets and a miniature. On desktop the sides alternate.
- **Miniatures:**
  - The Pass, The Spike and The Roll are **reused unchanged** from the landing's showcase (§6.7).
  - People, Venues & plaques and Rules are **NEW**, limited to what the code shows today: no counts in the
    People chips, no last tap, no QR pattern, statuses in database words, rule phrases from
    `internal/domain/tenant/rulebook.go`.
  - The tap stage reuses the `#team` phones.
- **Claims:** P2, P3, P5–P14, P16–P19, P21–P29 (25 NEW). Reused verbatim: LandingSteps[2], the LP panel
  lines, LandingComparison row 3, includes[4]. The H1 (P2) rests on the **PROPOSED**
  `FactEveryDecidedTapIsRecorded` (M14-08 creates it), so M14-18 comes after M14-08.

## 15. `/pricing` (inventory A §3)

- **Structure:** header (Pricing current) → hero (`LandingPricingHeading`, `LandingPricingLede`, the
  landing's price card unchanged) → Examples (a team-size table + a frozen-month receipt drawing) →
  Billing FAQ (two-column `<details>`) → CTA → footer.
- **The team-size table is computed server-side.** The view carries `[]TeamExample{Headcount, Monthly}`,
  filled by **`exampleTeamMonthly(publishedPricePerEmployeeMonth, n)`** (`internal/handler/marketing.go:671`,
  integer cents) for a fixed headcount list (10 / 25 / 60 / 150 → €15 / €37.50 / €90 / €225 at today's
  €1.50). The 60 row is `exampleHeadcount`. There is no second copy of the price and no JS.
  `TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount` (or a sibling) is extended to **every row**, with
  a control that changes the published price and watches every row move. The receipt's "€90.00" uses the
  same cents arithmetic in the Billing screen's money format; the receipt is sample anatomy, labelled.
- **Claims:** R3–R5 and R8–R14 (10 NEW).
  - R9 states only the first half of the Billing screen's CountLine. The port either carries the dropped
    conjunct or pins the shortened sentence explicitly as a summary of CountLine (inventory A R9).
  - **Not claimed, deliberately:** how payment is taken, invoices, VAT, any price for the API.

## 16. `/security` (inventory B, S1–S36)

- **Structure:** header (Security current) → hero (the security H2 "Four proofs on every single tap." becomes
  the H1, plus a sample APPROVED docket that matches the landing) → the four proofs in depth (S3–S13, with
  figures: link anatomy, counter rows, session boxes, the /7 width scale, the GPS radius) → the decision
  ladder (S14–S21, S36) → flagged, never dropped (S22–S25) → immutability (S26–S28) → never collected
  (S29–S31) → isolation (S32–S34) → **EU hosting (S35, gated)** → CTA → footer.
- **Verbatim reuse:** `LandingEvidence` ×4, `LandingSecurityLede`, `LandingPrivacyNote`, and `LegalPages[3].Lede`'s
  cookie sentence.
- **Port note:** printed figures come from code, never literals: the debounce and the radius from config
  defaults, the trust constants from the engine, the /7 and /31 limits from `netx`, and the guardrail count
  ("Five of the eleven", S36) from `len(policy.Guardrails(…))`.

### 16.1 S35 — EU hosting: **PENDING USER DECISION**

S35 ("Where your records live." / "Hosted in the EU" / "The production server runs in Falkenstein,
Germany, and database backups are encrypted before they are stored, in an EU-jurisdiction bucket.") rests
on `deploy/README.md` (node fsn1 = Hetzner Falkenstein, measured; the R2 EU-jurisdiction bucket with rclone
`crypt`). The published Privacy page still lists "where the data is hosted and processed" as a pending fact,
and the backup half is only **"selected"** in `deploy/README.md`, not confirmed in operation. **Do not port
S35 until the user approves it.** M14-20 ships `/security` without the S35 card. If the user approves, a
follow-up adds the card with its Fact (`FactHostedInTheEU`, pinned to `deploy/README.md` the way
`FactPlaquesIncludedAndReplacedFree` is pinned to the handoff). Nothing else on the page depends on S35.

## 17. `/for/restaurants` and `/for/production` (inventory B, R1–R16 and P1–P11)

- **Both** pages are built on `LandingAudiences` (Kicker, Title, Points, Example, verbatim). Paragraph() is
  the hero lede.
- **Restaurants:**
  - nine venue tiles from the seed;
  - the overnight shift: Rusty Bar 18:00–02:00, IN 17:58 → OUT 02:04 = "8h 06m · one shift";
  - lateness against the tapped venue's hours;
  - a real `<table>` of venues with a `<caption>` ("Sample setup · from our demo data");
  - cover at another venue, as a note that stays off the employee's screen;
  - "never invents a check-out";
  - the Roll with the product's legend words.
- **Production:**
  - one plaque at the KM Plant gate and five department shifts from the seed;
  - lateness against the department shift, with the "Same minute, different yardsticks" caption;
  - department on every record, with the Pass/People filter;
  - a gate queue with per-person debounce (the repeat tap shows IGNORED, direction NONE);
  - gloves and flour, and one 64 px button.
- The industry pages mark no header item as current.

## 18. NEW CLAIM groups and their Facts

There are about **120 NEW CLAIM rows**: landing 22 + `/product` 25 + `/pricing` 10 (inventory A) and
`/security` 36 + restaurants 16 + production 11 (inventory B). Some rows hold several sentences that rest
on one fact. The rule for every row is the same as M14-08: a value in `landingview.go` (or a per-page view
file in `pages`), the Fact or Anchor it rests on, a **derivation from code** (not a literal), a **mutation
control** that removes the code fact and watches the test go red, and a **rendered** check
(`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` and `TestFactMechanisms_SayNoWhenTheFactIsAbsent`
then cover each new Fact). Sample data, figure labels and honesty lines are furniture.

| Group | Rows | Existing Facts / Anchors reused | New (proposed) Facts |
|---|---|---|---|
| The employee's screen | T2–T9, P5–P7, P11 (prod.) | `FactTapPageNeedsNoApp`, `FactTapPageGreetsByName`, `FactTapButtonSizedForAWetHand`, `FactPositionReadOnlyOnPress` | `FactTapScreenHasOneButton`, `FactTapScreenNamesTheTappedVenue`, `FactCountedTapClosesWithoutAButton`, `FactFlaggedResultSaysItNeedsApproval`, `FactRepeatTapIsRecordedNotCounted`, `FactResultIsAStampedVerdict` |
| Records are kept | S6, P2, S4, S22–S25, P8 (prod.), P14 | `FactFlaggedQueueDecidedByAManager`, `FactCopiedLinkIsRefused` | `FactEveryDecidedTapIsRecorded` (M14-08), `FactRejectedTapIsRecorded`, `FactIgnoredTapIsRecorded`, `FactAwaitingHoursHeldOut`, `FactReviewOnceAndNotOwn`, `FactReviewIsASeparateRow`, `FactOpenCheckInsAreNotCounted` |
| Immutability and manual entry | P9, P10, S26–S28, P21 | `FactManualEntryNamesAManager` | `FactRecordsAreImmutable` (= `FactTransactionsImmutable`, **one** Fact, not two), `FactManualEntryIsAudited` |
| Onboarding and people | S2–S5, P17–P19, S6 (sec.) | `FactPeopleAreInvitedFromTheDashboard`, `FactDeactivatedPersonIsRefused` | `FactActivationTapRecordsNoHours` (= `FactActivationCompletesOnFirstNFCTap`, one Fact), `FactRosterStatusWords`, `FactInviteLinkShownOnce`, `FactDeactivatedTapRaisesAlert` |
| Review | S7–S8, P11–P13, S23 | `FactFlaggedQueueDecidedByAManager` | `FactSpikeShowsEvidenceAndReason`, `FactReviewTakesANote`, `FactNoSelfReview` |
| Reports and payroll | S9, P14–P16, R4, R12, R15–R16, P3–P5 (prod.) | `FactHoursExportAsCSV`, `FactBillingExportAsCSV`, `FactReportPerPersonAndVenue`, `AnchorLatenessFollowsDepartment` | `FactLatenessAgainstOwnShift` (= `FactLatenessUsesTheTappedVenuesShift`, one Fact), `FactShiftCountsOnItsStartDayAndVenue`, `FactReportCountsOtherVenueShifts`, `FactRollCountsLateArrivals`, `FactPassReadsAnyDay` |
| Venues, plaques, network | P22–P25, S9–S10, R6–R8, P10 (prod.) | `FactVenuesAndDepartmentsCarryOwnHours`, `FactPlaqueCanBeReplaced`, `AnchorVenueShiftAndAddress`, `AnchorPlaqueBelongsToVenue`, `AnchorDepartmentShift` | `FactTooWideAddressListIsNoEvidence` (= `FactTooWideListIsNotEvidence`, one Fact), `FactIPIsTheRequestSource`, `FactWifiNameIsNotEvidence` |
| Rules and the decision | P26–P29, S1, S14–S21, S36 | `SourceGPSRadiusDefault`, `FactCodelessTapNeedsAddress` | `FactRulesAreOwnerEditableAndAudited`, `FactGuardrailsCannotBeSwitchedOff`, `FactOnlyOwnerEditsRules`, `FactUndecidedTapIsReviewed` (= `FactUndecidedTapIsFlagged`, one Fact), `FactDecisionIsServerSide`, `FactDecisionOrder`, `FactGuardrailRows` (derived from `policy.Guardrails`), `FactBaselineRows` (from `policy.Baseline`), `FactTrustScoreIsInformational`, `FactGPSOutsideRadiusIsReviewed`, `FactDebounceIsPerPerson`, `FactDebounceUsesServerClock` |
| Crypto and sessions | S3–S5, S7 | `SourceSUNURLCarriesCounterAndSignature` | `FactChipLinkCarriesCounterAndCMAC`, `FactPlaqueKeysAreWrapped`, `FactSessionTokenStoredAsKeyedHash` |
| Privacy and isolation | S29–S34 | `FactNoAuthenticatorAPI`, `FactPositionReadOnlyOnPress` | `FactTenantIsolationByRLS`, `FactOperatorAccessIsAudited`, `FactHoldingIsTheOnlyCrossing` |
| Cross-venue cover | R1, R9–R11 | — | `FactCrossVenueTapJudgedLikeHome`, `FactCrossVenueNoteStaysOffTheEmployeesScreen` |
| Absences (tripwires) | R13 | — | `FactNoAutomaticCheckout` (an absence Fact, like `FactNoBulkImport`) |
| Integrations | I2–I7, R14 (pricing) | `FactAPIWritesPeopleAndLocations`, `FactAPIFeedsEveryPunch`, `FactAPIEnabledPerBusinessByTaptime` (from the FAQ-API fix, in `main`) | `FactWebhookIsOneSignedEndpoint` |
| Billing | R3–R5, R9–R13 | `FactPricePerEmployee` | `FactMonthIsHeadcountTimesPrice`, `FactFreeMonthsChargeNothing`, `FactHeadcountIsEmploymentOverlap`, `FactEndedMonthIsCountedOverItsOwnDates`, `FactFreezeIsFinalAndOwnerOnly`, `FactBillingNamesFirstChargedMonth` |
| EU hosting | S35 | — | `FactHostedInTheEU` — **gated, §16.1** |

**Merge the duplicates.** The two inventories were written in parallel and sometimes name one fact twice;
the "=" pairs above are one Fact each. The port declares one, and both pages' sentences rest on it. A
proposed name is a suggestion; the builder may rename it.

**Facts from the FAQ-API fix** (`FactAPI*`) exist only after `main` (with `0e7c158`) is merged into the work
branch. Every card that uses them depends on that merge.

## 19. Port feasibility (inventory B table + inventory A)

No figure needs JavaScript: there is no animation, toggle or measurement on the new pages, and the phone
Menu is `<details>`. Data-driven geometry becomes either **inline SVG with presentation attributes computed
in Go** (the `operatorpages/qr.go` pattern) or **fixed class buckets written literally in templ**:

| Figure | Port | New pinning test (described; the builder names it) |
|---|---|---|
| Overnight timeline (restaurants) | inline SVG; x/width from the seed venue shift and the two sample times | the bar is Rusty Bar's shift from the venue fixture, and "8h 06m" is the ledger's duration format of OUT−IN |
| Department shift bars + 04:02 marker (production) | inline SVG (one `<rect>` per department) or literal bucket classes for start × length | the bars match the seed departments; the late caption is `tap.MinutesLate` (04:02 vs 04:00) in the reports label format |
| /7 width scale (security) | inline SVG; the limit's x from netx's constants | the page prints netx's IPv4/IPv6 limits and no other prefix |
| GPS figure (security) | inline SVG (unchanged); "radius: 150 m by default" from `TAPPA_GPS_RADIUS_M`'s default | the radius is the config default |
| Roll miniatures (landing, product, restaurants) | the product's `.roll-cell--worked/--other/--open/--none` looks, re-scoped for the public CSS | the legend words equal `reports.templ`'s rollLegend |
| Trust card (20 / +50 / +30) | values from the engine's trust constants | the trust figures match the engine |
| "60 seconds", "150 m" | printed from config defaults, with the "default" caveat | the printed defaults come from config |
| Guardrail rows + "Five of the eleven" | count from `len(policy.Guardrails(…))` | the `FactGuardrailRows` test |
| Team-size table (pricing) | `exampleTeamMonthly` per row, server-side | the extended example-cost test, every row |
| Two-phone figures (`#team`, product 01) | fixed `.lp` classes per breakpoint, stacked at 390 | — (CSS) |
| Docket tilts, rail clips, the Menu's open/closed label | `nth-child` classes; `details[open]` CSS | — |

The verbatim inventory text for each row is in [`lp6-inventory-b.md`](lp6-inventory-b.md) ("Port feasibility")
and [`lp6-inventory-a.md`](lp6-inventory-a.md) (§3 computed values, §5 rules checked).
