# Panel redesign "The Pass" — handoff for the port

This folder is the handoff package for porting the business panel's screens from the approved
design ("The Pass", 2026-10-20) to templ + Tailwind. A developer agent should be able to port
every screen from this README, the six screen files and the M12 task cards without discovering
surprises in the code.

- **Canvas:** <https://claude.ai/artifact/T1t7icoKDSixrxM54QG8LT> (private to the owner; the
  same boards are in [`boards/`](boards/), one `.dc.html` file per board, `canvas.json` lays
  them out).
- **Screen files:** [`screens/`](screens/) — one per group, each the code-cited analysis edited
  to the user's decisions: [pass-ledger](screens/pass-ledger.md) ·
  [spike-odd](screens/spike-odd.md) · [people](screens/people.md) ·
  [venues-roll](screens/venues-roll.md) · [rules-billing](screens/rules-billing.md) ·
  [account-auth](screens/account-auth.md).
- **Task cards:** [`docs/plan/m12-panel-redesign.md`](../../plan/m12-panel-redesign.md)
  (M12-01 … M12-50, ordered, each independently shippable; M12-50 is a P1 defect card numbered last by the house ID rule).
- **Already shipped:** the shell and navigation — `0bb5085` (port) + `dfb122d` (audit fixes).
  The sidebar / rail / bottom bar, the `<details>` More sheet, the section labels and the
  `.panel-*`, `.side-*`, `.bar-*`, `.sheet-*` classes are done and are not re-specified here.

All `file:line` citations in this folder refer to **HEAD `b284899`**. Lines move; re-find them by
the symbol named beside the line number.

---

## 1. How to read a board (`.dc.html`)

A board is a self-contained mock-up page, not a template:

- `<helmet>` loads Google Fonts and a `<style>` block with board-only classes (`.lbl`, `.docket`,
  `.stamp--ok`, `.navlink` …). **None of this is product CSS.** The product self-hosts its fonts
  (no runtime link to any external host) and its classes live in `web/static/css/input.css`.
- The markup uses `{{binding}}` placeholders, `<sc-for>` / `<sc-if>` loops and conditionals, and
  **inline `style="…"` on almost every element** (93 on `Main.dc.html` alone).
- A trailing `<script type="text/x-dc">` holds a `Component` class whose `renderVals()` returns the
  **sample data**: names, venues, counts, sentences, tilts, filter state. Sample data is never
  product copy and never product fact — every figure and sentence on a screen comes from the code
  (see the binding copy tables in the screen files).
- `data-props` on that script exposes design-time tweaks (e.g. an accent picker). They are
  states to document, not UI to build.

**Never copy an inline style.** The panel's CSP is `style-src 'self'` with no `'unsafe-inline'`
(`adminCSP`, `internal/handler/adminlogin.go:1782`), so a `style=` attribute is blocked in the
browser. Translate every inline style into Tailwind utilities written literally in a `.templ`
file, or into a component class in `input.css` (§3 constraint 1). Data-driven sizes become fixed
class buckets (§4) or inline SVG attributes.

---

## 2. The user's decisions (2026-10-20) — binding

Full text: the decisions note handed to this port (summarised here; the screen files apply it row
by row).

### D1 — Code is the truth (*"Kod gerçektir"*)

Where a board and the product disagree, the product wins. **The port moves the look; it adds no
product feature.**

**Removed from the boards and the spec** (each screen file lists its own under "Not ported"; the
backlog keeps them under *"Panel redesign — drawn but not built"*):
header search · "of N rostered" / "not in yet" / "rostered, still ahead" (there is no roster) ·
tenant switcher counts and "Switch" · "Unlink phone" · "Edit details" · Account managers card ·
holding/group card · "Order plaques" · "Mark lost" · plaque position names ("Front door" — no
column; show the UID) · "REPLACE" plaque status (use the code's states plus the key alert) ·
keyboard shortcut hints (A/R) · the per-rule Approve / Spike / Reject picker (the real model is:
switch a rule off/on, write your own copy `own:*`, bind/unbind — one confirmation per change) ·
live refresh · "Copy" buttons (they need JS — use selectable text) · masked `tt_whsec_` tail (the
key is stored sealed and never shown again) · latency "in 312 ms" · distance in metres · payment
card / billing e-mail / invoice PDF / per-person breakdown · per-row "Close it" on Odd shapes ·
account closing · Spike "her/she" pronouns (neutral wording) · demo controls (Mockup buttons,
Replay, preview toggles; tweak-only states are documented as states, not UI).

**Kept, even where a board dropped it:** test-pinned copy · §4.6 quantities (denominators,
awaiting/refused hours, "what this page does not see", truncation notices, the lost-plaque alert,
the masked-check-in tally) · the stock plaque list · the full plaque uid and its open link · the
password form and "what your staff read" on Account · the T125 legacy-baseline caveat on Rules ·
the signed-in / sign-out block · the sign-in failure sentence (it names no cause) · the
enumeration-safe reset wording.

**Product facts the boards got wrong — corrected to the code:**
11 guardrails in the engine's own order (incl. `sys:tap-freshness`, `sys:occurred-at-bound`,
`sys:policy-edit-owner-only`, `sys:no-self-review`) and 9 baseline documents (incl.
`base:queued-window` and the owner/manager permission document) · "first match wins" applies to
guardrails only; below them the most specific scope wins, then the strictest effect, then the
engine default (The Spike for taps) · the locked last rung is the engine **default**, not
`base:no-evidence-review` · a **calendar day**, not "06:00 → 06:00 shift day" · hours in the code's
`8h 02m` format · database status words (INVITED, ACTIVE, DEACTIVATED; "In service" for plaques) ·
queue order **newest first** · signup has **3** steps · free months come from
`FirstChargeableMonth` (**3**, not 2) · the pair threshold is **20 s per venue over 2+ days** ·
"verified by GPS alone" · the GPS radius is configuration (`TAPPA_GPS_RADIUS_M`, default 150 m) ·
the Wi-Fi name is **not evidence** · a QR scan needs the venue network (`base:qr-requires-ip`) · a
manual entry starts from a person and returns to the record on The Pass (no "3 Recorded" step) ·
lateness is measured against the department shift, else the **tapped** venue's shift ·
deactivation keeps sessions alive · Legal is a **public** page (marketing chrome), not a panel
screen · Account shows Developers only as an **owner-only link card** · there is no 401 page.

Requested-but-missing features go to the backlog as separate items, not into this port.

### D2 — Corners and chips (*"Kuralı güncelle"*)

The kitchen docket and the stamp stay **sharp** (`rounded-none`, at most `rounded-sm`) — the
signature motif. Cards, form fields, buttons, chips and sheets may be rounded as drawn
(`rounded-xl` … `rounded-2xl`, `rounded-[18px]` for cards); chips are `rounded-full`. The
`tappa-brand` skill says so (dated note, 2026-10-20).

### D3 — New reads: build all of them (*"Evet, hepsi"*)

New sqlc queries on **existing** tables (no schema change), each its own task through
`tappa-db-migrator`, each with an RLS test, an explicit `tenant_id` filter, its entry in the
evidence-reader / employee-location-reader inventories, and holding (K-39) handling:

| Screen | Read | Card |
|---|---|---|
| The Pass | "on the floor now" (latest open check-in within `tap.StaleOpenIn`) + per-venue tally | M12-13 |
| The Pass | hourly tape for elapsed hours (people on shift per hour) | M12-14 |
| The Pass | left-open count (reuses `ListOpenCheckIns`) | M12-15 |
| The Pass / Ledger | day verdict counts for the summary and chips | M12-16 |
| People | counts per status | M12-17 |
| People | job title and last tap per person in the roster | M12-18 |
| People | last 7 taps of one person | M12-19 |
| People | per-person summary (do **not** widen `GetPanelEmployeeForAction`) | M12-20 |
| The Roll | role + home venue in the worked-shift read; `EmployeeID` on `OpenEntry`; other-venue / manual / open cell marks in `ledger.accumulate` | M12-21 |
| Odd shapes | evidence split per venue (both / network only / distance only / neither — neutral field names) | M12-22 |
| Odd shapes | per-day dots for pairs | M12-23 |
| Odd shapes | plaque counter from → to | M12-24 |
| The Spike | "decided today" count on `transaction_reviews` | M12-25 |

**Never show 0 for something not measured.** Every new figure has a "measured" and a "could not
read" state (the `PendingBadge` / `PlaqueCounts.Queried` pattern). A failed widget read renders
"could not be read" (or nothing) and does not fail the page.

### D4 — Real QR (*"Gerçek QR"*)

The plaque drawing on Venues shows the plaque's **real** QR (`<base>/t?tag=<uid>`), rendered on
the server with `internal/qrcode` as inline SVG, the way `web/templates/operatorpages/qr.go`
does. Never a fake pattern. The view type must not carry `[][]bool` (the plaque key-shape walk
refuses slices) — pass a pre-rendered path string. Card M12-12.

### Existing defects found by the analysis (each fixed first, as its own card)

| # | Defect | Card |
|---|---|---|
| 1 | Billing "Freeze <month>" form posts no `month` field → always `?problem=unreadable` (`billing.templ:149-151` vs `readBillingMonth`) | M12-02 |
| 2 | `Venue.HasProofOfPlace()` (`internal/domain/tenant/venue.go:316-318`) ignores the read-side /7 · /31 rule, so "IP ✓" and the missing "No proof of place" warning can describe a list that is not evidence | M12-03 |
| 3 | The sign-in proof list still claims a practice tap (removed by ADR 0026) | M12-04 |
| 4 | Reset copy "Ask another owner … set a new password for you from the panel" — no such feature | M12-05 |
| 5 | Signup passes an empty sign-in href (`marketingChrome("")`) | M12-06 |
| 6 | Signup step 3 "You can invite the rest of your managers later" — manager invites are blocked (M7-07 / Q02) | M12-07 |
| 6a | Rules: `guardrailRules` (`internal/domain/tenant/rulebook.go:813-816`) tells the business the `sys:tap-freshness` window and the `sys:occurred-at-bound` tolerance are "yours to set"; they are set by Taptime (the settings block already says so, `policies.templ:261`; backlog T72). Boards show the corrected sentence. | M12-50 |
| 7 | *Not a defect (D1):* manual entry renders under The Pass tab (`manualentry.go:691`); the boards are corrected to show The Pass as current | — |

---

## 3. Constraints every screen inherits

1. **CSP: no inline style.** `style-src 'self'`. No `style=`, no `<style>`, no `data:` URI in CSS
   (`img-src` is `'none'` unless a logo is drawn). Data-driven geometry becomes **class buckets**
   written literally in a `.templ` file (§4: width 0–100 % in 21 steps, tape heights, tilts via
   `nth-child`) or **inline SVG presentation attributes** (`x`, `width`, `d` — the shift bar, the
   QR).
2. **Tailwind scans only `web/templates/**/*.templ` and `web/static/js/**/*.js`**
   (`tailwind.config.js` `content`). A class name built in a `.go` file produces no CSS. Choose
   classes in templ `switch`es or in small Go funcs **inside** a `.templ` file. Never write a
   utility name in a templ **comment** — Tailwind compiles it (the brand skill measured seven dead
   rules born that way); describe it instead. `@layer components` classes are purged unless a
   `.templ` uses them literally — **and the test is lexical, not a use count**: a layer class whose
   name is a word that stands alone in any template (prose, a Go keyword) compiles with no element
   carrying it. Measured in M12-08: `.chip`, `.field`, `.lock` and `.switch` compiled at once
   (+2 479 B for the four names alone) — so those four are `.chip-link`, `.field-stack`,
   `.lock-glyph`, `.rule-switch`, and a selector that also names a class in use
   (`.rail-item … .docket`) compiles with it (the `.docket`-bound tilt rules added +390 B more;
   +2 869 B together). Name a new class something no sentence would contain.
3. **No new JavaScript.** Only `/admin` (The Pass / ledger) carries a script (htmx), pinned by
   `TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty`; only the sign-in page carries
   `adminpassword.js`. Selection is a link (`?focus=`, `?manage=`, `?at=`); multi-step is a POST
   form; disclosure is a native `<details>`. No `hx-on`, no `js:`, no event filters
   (`TestPanelMarkup_UsesNoHtmxSyntaxThatNeedsUnsafeEval`). No `href` containing `://` on a panel
   page.
4. **IBM Plex Mono is self-hosted at 400 and 700 only.** Every board number drawn at weight 500
   becomes `font-normal` (400). Adding a weight is a fonts README + licence change.
5. **Shared components that also render the employee tap / activation screens do not change:**
   `components.Notice`, `components.Card`, `pages.Problem`, every `layout.*`. Their bytes are pinned
   (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`,
   `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`). Add panel-only components instead:
   `PanelNotice`, `PanelEmpty`, `PanelProblem` (M12-09, M12-10).
6. **Shell hooks first (M12-01).** `PanelChrome` gains optional **Kicker**, **Title**, **Crumbs**
   and **Actions** slots; the shell still prints exactly **one** `<h1>` (the Title when set, else
   the section label); h1 size `text-3xl md:text-4xl desk:text-[44px]`. Content width is
   revisited: the well is `max-w-5xl` (1024 px) today; at `desk` = 1200 px the content box is only
   **864 px** wide (1200 − 248 sidebar − 2 × 44 padding), at 1440 px it is 1104 px. **Every desk
   recipe must fit 864 px**; the cap may rise to about 72 rem.
7. **Holding (ADR 0027).** Partner venue names come only through the existing
   `holding_site_locations` path (`siteVenuesFor`, `internal/domain/ledger/site.go:47-64`). K-39
   evidence hiding stays in the ledger (`nameSiteRecords`), never in a template: partner rows show
   the outcome, never trust, IP/GPS, distance, rule sid or an evidence sentence; partner rows never
   enter an evidence count; the rule-distribution partner bucket stays anonymous and last. Adding a
   `holding_*` function or widening one is a user decision + ADR 0027 change — this port needs none.
8. **Never show 0 for something not measured** (§4.6, the class M5-11 closed). Tri-state every
   figure: not queried → claims nothing; failed → "could not be read" / "?"; capped → "N+" and a
   sentence. Empty states stay gated on `Queried`.
9. **No coordinate travels.** View and domain types must not grow a field whose name the walkers
   refuse (`gps`, `lat`, `lng`, `coord`, `addr`, `ip`, `source`, `where`, `geo`, `key`, `code`,
   `context`, `token` …; `bannedFieldName`, `internal/handler/reports_test.go:488-506`, and
   `TestTypes_NothingOnTheReadPathCanCarryACoordinate`). Use the codebase's vocabulary: `Both`,
   `NetworkOnly`, `DistanceOnly`, `Neither`, `HomeVenue`.
10. **No float for hours or money** (§6). Integer `time.Duration` math; integer percents
    (`n*100/of`), rounded to 5 for width buckets; counts printed beside every bar so rounding
    loses nothing.
11. **Touch targets ≥ 44 px.** Every new pressable class (chip, roster row, queue item, switch,
    field) is registered in `panelTouchTargets` (`internal/handler/dashboard_test.go:915`) and in
    the compiled-CSS 44 px check. Until a screen draws it, it is not in app.css, so M12-08 lists it in
    `panelPendingTouchTargets` (heights read from `input.css`); **the screen card that first writes
    it moves it to `panelTouchTargets`** — the pending test is red until it does. *(M12-48: every
    class M12-08 listed has been moved; the pending map is empty by design and stays as the way in
    for a press target defined ahead of its screen.)*
12. **Status is never colour alone.** Every tone carries a word or an `sr-only` word; marks
    (✓ ✗ –) are `aria-hidden`. Text is at least `ink/70` (6.05:1 on paper); `ink/50` is for
    borders only (3.23:1 on paper fails AA).
13. **Accent slots do not grow.** The tenant accent paints only the 4 px panel stripe and the
    Account preview's tap button (`TestCompiledCSS_BrandVariablesOnlyInTheirSlots`,
    `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` — a new class with `brand` in its name breaks
    the latter).
14. **Goldens.** Holdingless goldens (reports, anomalies), key-budget goldens (The Pass) and the
    WL-8 panel digests change when their markup changes. Re-render them **in a dedicated commit**
    whose message carries the written byte-check (the `0bb5085` method: each page's own markup with
    the chrome cut off compared to the old golden; the holding/holdingless pair identical apart
    from the partner section; an absent banner still adds 0 bytes).
15. **Tests during a task:** only the changed tests with `-run`, plus `gofmt`, build and
    `scripts/redline-check.sh`. The full suite and `make check` run once, before the commit
    (CLAUDE.md §10). `make gen` after every `.templ` / `db/queries/*.sql` edit; generated files are
    committed.

---

## 4. Token map

Palette only (brand skill): `ink #152219` · `porcelain #EDF0EA` · `paper #FFFDF4` ·
`tappa-green #1F5C41` · `green-lite #E1EDE6` · `saffron #D98E2B` · `saffron-lite #F7EBD6` ·
`tomato #BE3D2A` · `line #C9D2C8`. Any other hex on a board (e.g. `#9A5F14`, a sample
`brand.Suggest` output) is sample data.

### Opacity steps

For **new** classes use Tailwind's default steps only: **5, 10, 15, 20, 25, 40, 50, 70, 75, 80** (existing classes keep their steps — e.g. the `text-ink/85` help and note text in `fieldNote` and the docket stays as it is). Round a board alpha
to the nearest of these:

| Board alpha | Class step |
|---|---|
| .04 · .05 · .06 · .07 | `/5` |
| .08 · .10 · .12 | `/10` |
| .14 · .15 · .16 | `/15` |
| .18 · .20 | `/20` |
| .25 | `/25` |
| .40 · .45 · .50 · .55 | `/50` (borders); `/40` only if a measured contrast needs it |
| .70 | `/70` |
| .75 · .80 | `/75` · `/80` |

### rgba → class

| Board value | Class | Note |
|---|---|---|
| `rgba(21,34,25,.05)` / `.06` | `bg-ink/5` | rule-bar track on phone, plate fill |
| `rgba(21,34,25,.5)` | `border-ink/50` | **borders only** — as text use `text-ink/70` |
| `rgba(21,34,25,.7)` | `text-ink/70` | labels, help, meta (6.05:1 on paper) |
| `rgba(21,34,25,.10)` / `.18` shadow | `shadow-ink/10` / `shadow-ink/20` | plaque figure, sticky bars |
| `rgba(31,92,65,.06)` | `bg-tappa-green/5` | ✓ evidence tile, row hover |
| `rgba(31,92,65,.08)` / `.1` / `.12` | `bg-tappa-green/10` | selected roster row, ok pill, ok notice |
| `rgba(31,92,65,.14)` / `.15` | `bg-tappa-green/15` | worked day cell, switch on |
| `rgba(31,92,65,.55)` | `bg-tappa-green/50` | "network only" segment (legend uses the same) |
| `rgba(217,142,43,.1)` / `.12` | `bg-saffron/10` | gap chip, open chip, invited pill |
| `rgba(217,142,43,.14)` | `bg-saffron/15` | diff "before" pill |
| `rgba(217,142,43,.18)` | `bg-saffron/20` | — |
| `#F7EBD6` | `bg-saffron-lite` | Spike card, warn notice, default plate |
| `rgba(190,61,42,.06)` / `.07` | `bg-tomato/5` | alert box, danger card, ✗ tile |
| `rgba(190,61,42,.1)` / `.12` | `bg-tomato/10` | failed delivery pill, refused read-back row |
| `rgba(255,253,244,.4)` | `border-paper/40` | secondary button on ink |
| `rgba(255,253,244,.7)` | `text-paper/70` | tagline / meta on ink (8.5:1) |
| `#C9D2C8` as text on ink | `text-line` | 10.62:1 on ink |
| `#C9D2C8` 1 px dashed | `border-dashed border-line` | row separators, docket rules |

### Radius (D2)

| Shape | Class |
|---|---|
| docket, stamp, receipt, mini docket | `rounded-none` (max `rounded-sm`) |
| card, sheet, drawer, form card | `rounded-[18px]` (put it once in a component class, e.g. `.panel-card`) |
| notice, venue card, queue item, evidence tile, stepper | `rounded-xl` / `rounded-[14px]` |
| field, small button | `rounded-[10px]` |
| primary / large button | `rounded-xl` … `rounded-2xl` |
| chip, segmented chip, status pill, week dot | `rounded-full` |
| small code chip, rung badge | `rounded-md` |

### Type sizes

Display (Space Grotesk): h1 `text-3xl md:text-4xl desk:text-[44px] leading-none tracking-[-0.035em]`
(shell slot) · card h2 `text-lg`…`text-xl font-bold` · body `text-sm`/`text-[15px]`.
Mono (IBM Plex Mono, 400/700): eyebrow / field label `.lbl` = `font-mono text-[11px] uppercase
tracking-[0.12em] text-ink/70` (or reuse `.docket-label`, 10 px, whose AA grounds are pinned) ·
big figures `font-mono font-normal leading-[0.9] tracking-[-0.04em]` at `text-5xl` … `text-[88px]`.

---

## 5. Component catalogue

Each entry says what it **extends** (an existing class or templ component) or that it is **new**,
where it lives, and its breakpoint rule. Breakpoints: base = phone (< 768), `md:` = tablet
(768–1199), `desk:` = desktop (≥ 1200, `screens.desk`). Scroll bleeds use the shell's padding:
`-mx-4 px-4 md:-mx-8 md:px-8 desk:mx-0 desk:px-0` (`.panel-main px-4 md:px-8 desk:px-11`).
Repeated chains become `@layer components` classes in `input.css` (the report-row measurement,
`input.css:704-713`: on a `no-store` page the class attributes were most of a row's weight).

| Component | Extends / new | Recipe | Breakpoints · notes |
|---|---|---|---|
| **Docket** | extends `.docket` (`input.css:132-142`, perforation top + bottom) and `components.TransactionDocket` (`components/docket.templ:32-176`) | keep the class; padding `px-4 pt-4 pb-3.5` — **not applied (M12-27, recorded deviation):** the full docket keeps `.docket`'s own `px-4 py-3`, because `TestTransactionDocket_IsADocketAndCarriesEveryFieldTheCardAsksFor` pins the attribute as exactly `class="docket"` and `.docket` is shared with the employee result screen (§3.5), so the 4 px / 2 px difference is not worth loosening that pin; `dl` `grid-cols-2 desk:grid-cols-4`; ctr exactly as the code prints it, unpadded (`#612` — `strconv.Itoa`, `internal/handler/transactions.go:273`, `docket.templ:107-108`; D1); label "· partner venue" when `PartnerVenue` | **Sharp corners** (D2). One responsive component, not two copies: full `dl` `hidden desk:grid`, compact evidence line `desk:hidden`. Keep the "Manager's note" separate from the engine note at every width; keep "Entered by a manager"; partner rows print "not shown at a partner venue". Signs stay the code's words ("IP yes · GPS no", "n/a"). M12-11 |
| **Rail docket** (compact) | new `components.RailDocket(DocketView)` over `.docket` | `article.docket -mt-1 flex flex-col gap-2 pt-4 desk:pt-[18px]`; name `font-display text-[17px] desk:text-lg font-bold truncate`; direction box `border border-ink px-1.5 text-xs font-bold uppercase`; time `font-mono text-xl desk:text-[22px]`; foot `border-t border-dashed border-line pt-2 text-[11px] text-ink/70` | width `w-[190px] md:w-[200px] desk:w-[214px]`. Manual and TRAINING stay distinguishable; K-39 phrase on partner rows. M12-11 |
| **Stamp** | existing `.stamp` + `stamp--approved/flagged/rejected/ignored/training` via `verdictStamp` (export it as `VerdictStamp`) | never copy classes; rotation comes from the class, not the board's −8° | **Sharp** (D2). Word ink, colour in the frame. Board `stamp--ok/flag/reject` are board names. |
| **Rail + clip** | new | scroller `overflow-x-auto pb-[18px]` + bleed; inner `relative min-w-max pt-2.5`; bar `absolute inset-x-0 top-2.5 h-[7px] md:h-2 rounded bg-ink` (`aria-hidden`); list `ol.flex gap-3 md:gap-3.5 desk:gap-4 px-3.5`; clip `h-[18px] w-10 md:w-11 desk:w-[46px] rounded-b-md bg-ink/70` (`aria-hidden`) | **Tilt via `nth-child`** in `input.css`: `.rail-item:nth-child(7n+1) article { rotate: -1.2deg }` … seven rules (shipped keyed on the
`article`, not `.docket` — §3 constraint 2) (−1.2, 0.8, −0.4, 1.1, −0.9, 0.5, −1.4), `transform-origin: top center`. Same rail + clip on Choose. |
| **Tally** | existing `.tally` + tones (`input.css:625-703`) | add `.pill-tally { rounded-full font-bold tracking-[0.08em] px-2 }` | tones: `--mounted` green · `--unencoded` saffron · `--stock` line · `--rejected` tomato · `--manual` · `--queued` · `--approved` |
| **Tally ticks** (floor per venue) | new | row `grid grid-cols-[84px_1fr_34px] md:grid-cols-[86px_1fr_34px] desk:grid-cols-[92px_1fr_28px] items-center gap-2`; ticks `font-mono text-sm tracking-[2px] text-tappa-green overflow-hidden` (`aria-hidden`); number `font-mono text-xs text-ink/70 text-right` carries the meaning | tick string built in Go (content, not classes); cap ticks at 20; **no denominator** (D1) |
| **Tape bars** | new | row `grid grid-cols-[74px_1fr] md:grid-cols-[88px_1fr] desk:grid-cols-[96px_1fr] items-end gap-2`; bars `grid grid-cols-[repeat(24,minmax(0,1fr))] items-end gap-0.5 h-7 md:h-[34px] border-b border-line`; done `bg-tappa-green`, now `bg-ink`; axis `font-mono text-[10px] text-ink/70` | **Height buckets:** ten literal classes `.tape-bar--1 … --10` in `input.css` chosen by a templ `switch` on a bucket int from Go. Calendar day axis 00 → 24, elapsed hours only, no future bars (D1). Accessible table (`sr-only`) or per-row `aria-label`. Scroller inner `min-w-[560px] md:min-w-[640px] desk:min-w-[680px]`. |
| **Stat card** | new `.stat-card` | `rounded-[18px] border border-line bg-paper p-4 md:px-5 md:py-[18px] desk:px-6 desk:py-[22px] flex flex-col gap-2.5 desk:gap-[18px]`; big number per §4 | Spike card: `+ border-saffron bg-saffron-lite` as an `<a>`; open card: `+ border-dashed border-ink/50`. Grid `grid grid-cols-2 gap-3 desk:grid-cols-4`, floor card `col-span-2`. Tri-state value. |
| **Chip / segmented** | new `.chip-link` (+ `--on`), `.chip-count`, `.chip-group` (shipped M12-08; `.chip` would compile from prose, §3.2) | chip `inline-flex min-h-11 flex-none items-center gap-1.5 rounded-full border px-3.5 font-display text-[13px] font-medium`; off `border-ink/50 text-ink hover:border-ink`; on `border-ink bg-ink text-paper` + `aria-current="true"`; count `font-mono text-xs opacity-80` | **`rounded-full`** (D2). Links, not buttons; labels from the validator's own slice (`panelVerdicts`, `panelStatuses`). Phone: horizontal scroller with bleed, `[scrollbar-width:none]`. Week/day **segmented** stepper: `inline-flex items-center rounded-xl border border-line bg-paper` with two `a.size-11` arrows (`aria-label`) and a mono label. |
| **Evidence tile** | new `.ev-tile`, `--ok/--bad/--none` | `flex flex-col gap-1.5 rounded-xl border border-line px-3.5 py-3` + `bg-tappa-green/5` / `bg-tomato/5` / `bg-paper`; value `font-display text-base font-bold`; sub `text-xs text-ink/70`; mark `size-[22px] rounded-md font-mono text-[13px]` `bg-tappa-green text-paper` / `bg-tomato text-paper` / `bg-line text-ink`, `aria-hidden` | grid `grid-cols-2 gap-2 desk:grid-cols-[repeat(auto-fit,minmax(150px,1fr))]`. Partner rows: one merged tile "Network · Location: not shown at a partner venue". No carrier, SSID, metres or "declined". |
| **Queue item** | new `.queue-item`, `--current` | `a.flex w-full min-h-11 flex-col gap-1 rounded-xl border border-line px-3.5 py-3 hover:border-ink focus-visible:ring-2 focus-visible:ring-ink`; current `border-2 border-ink bg-paper` + `aria-current="true"` | tablet `w-[220px] min-h-14` in a scroller; desk full width in a column; phone hidden (pager instead). Focus links keep `after_at` / `after_id`. |
| **Roster row** | new `.roster-row` (register) | `a.flex w-full flex-wrap items-center gap-3.5 border-t border-dashed border-line px-5 py-2.5 min-h-[60px] text-ink hover:bg-tappa-green/5 first:border-t-0`; selected `aria-[current=true]:bg-tappa-green/10 aria-[current=true]:shadow-[inset_3px_0_0_theme(colors.ink)]` | phone 64 px, meta "role · last tap", phone column `hidden md:block`, chevron `md:hidden`. Update the row-finding regex in the same commit. |
| **Status pill** | new `.pill` + tones (or a `pill` variant of `lifecycleTally` so the word has one source) | `inline-flex rounded-full border px-2.5 py-0.5 text-xs font-medium whitespace-nowrap text-ink`; active `border-tappa-green bg-tappa-green/10`; invited `border-dashed border-saffron bg-saffron/10`; deactivated `border-line text-ink/70` | **Database words** (INVITED / ACTIVE / DEACTIVATED). Word ink, colour in the frame. |
| **Initials square** | new | row `inline-flex size-10 flex-none items-center justify-center rounded-[10px] border border-ink/50 bg-paper font-mono text-[13px] font-bold`; drawer `size-14 rounded-[14px] border-ink text-lg` | `initials(name)`: first rune of first and last word, upper-cased, max 2 (Maltese ċ ġ ħ ż); `aria-hidden` |
| **Drawer** (person card) | restyles `components.RosterActions` | `flex flex-col gap-5 rounded-[18px] border border-line bg-paper p-6`; sections `border-t border-dashed border-line pt-4` | desk aside `desk:flex-[1_1_340px]`; `md`–`desk` above the list (`order-first desk:order-none`); phone card only, list hidden, "Back to People" |
| **Form field** | new `.field-stack` / `.field-input` (+ `--mono`, `.field-help`; error follows `aria-invalid`) (register; do not change `.filter-input`, other pages use it) | label `.lbl`; control `min-h-11 w-full rounded-[10px] border border-line bg-paper px-3 font-display text-sm text-ink focus:border-tappa-green focus:outline-none focus:ring-2 focus:ring-tappa-green/40`; mono variant for e-mail / VAT / ranges / ref; help `text-[13px] leading-[1.45] text-ink/70`; error `border-2 border-tomato` + `aria-invalid` + `aria-describedby` | keep `fieldNote` semantics and every field's reserved note line |
| **Buttons** | extend `.btn` (`input.css:264-271`) with modifiers, decided once | `.btn--ink` `bg-ink text-paper hover:bg-tappa-green`; `.btn--line` `border border-ink bg-transparent text-ink`; `.btn--danger` `border-2 border-tomato bg-paper text-ink`; `.btn--approve` `min-h-16 rounded-2xl bg-tappa-green text-lg text-paper hover:bg-ink`; `.btn--reject` `min-h-16 rounded-2xl border-2 border-tomato bg-paper text-lg text-ink hover:bg-tomato/10`; radius `rounded-xl` | accent never applies; ink word on tomato frame for destructive |
| **Venue card** | new `.venue-card`, `--on` | `a.flex w-full flex-col gap-2.5 rounded-[14px] border border-line bg-paper px-4 py-3.5 hover:border-ink focus-visible:ring-2 focus-visible:ring-ink`; selected `border-2 border-ink` + `aria-current="true"`; evidence badges `.ev-badge--ok/--none/--wide` | list `grid grid-cols-1 gap-2.5 md:grid-cols-2 desk:grid-cols-1`; phone links `?at=<id>#venue` |
| **Plaque drawing + real QR** | new `components.PlaqueDrawing(PlaqueDrawingView{Last4, QRPath, QRSide})`; QR via `internal/qrcode` (`Encode`, `Bitmap(QuietZone)`) and a new SVG-path helper **in `internal/qrcode`** (one implementation, round-trip tested there; the panel must not import `operatorpages` and keeps no copy) | figure `flex flex-col items-center gap-3 rounded-[18px] bg-line/50 p-6`; plaque `w-[200px] rounded-2xl border border-line bg-paper`; tap zone with three `aria-hidden` rings, "taptime", **"Hold your phone here"**; QR half `border-t border-dashed border-line`, `<svg viewBox shape-rendering="crispEdges" aria-hidden="true">` with `fill-paper` rect + `fill-ink` path; **"Or scan — same thing"**; `PLAQUE 91AC` (last 4 of the uid); caption "drawing, not to scale" | D4. Ink on paper, never on green. URL `<base>/t?tag=<uid>` from one helper beside `sun.MintTapPath`; no `?src=qr`. Figure `role="img"` + `aria-label`. No plaque on a wall → no drawing + "No plaque is on a wall here yet". |
| **Plaque table row** | restyles `components.PlaqueRow` | `tr border-t border-dashed border-line`; `th scope=row` uid (full uid reachable); mono cells `px-2 py-3 font-mono text-[13px]`; open link `inline-flex min-h-11 items-center px-3 underline` | `overflow-x-auto`, table `min-w-[620px]` from `md`; phone reflow of the same DOM (no second copy) |
| **Receipt** | reuses `.docket` (perforation already top + bottom) | `px-6 pt-6 pb-[26px] flex flex-col gap-3.5`; title centred `font-display text-lg font-bold`; lines `flex justify-between gap-2 font-mono text-[13px]`; total `font-mono text-3xl font-normal` | **Sharp.** Do not add a second perforation implementation (board `.receipt` is the same motif). Billing receipts, The Roll's receipt, Freeze month. |
| **Day cell** (The Roll) | new `.roll-cell` + `--worked/--other/--manual/--open/--none` | base `inline-block min-w-[58px] rounded-lg py-[7px] text-center font-mono text-[13px] text-ink`; worked `bg-tappa-green/15`; other `+ ring-1 ring-inset ring-ink`; manual `+ underline decoration-dotted` + literal `M`; open `bg-saffron/10 border border-dashed border-saffron`; none `text-ink/70` "—" | modifiers combine; each carries `sr-only` words; phone reflow `max-md:` variants; legend with the same tone classes |
| **Ladder rail / rung / riveted plate** | new `.ladder`, `.rung`, `.rung-no`, `.plate`, `.lock-glyph` — **class-only** | rail `.ladder::before` 8 px ink bar; rung connector `.rung::before`; badge `.rung-no` `w-[52px] h-7 rounded-md bg-ink text-paper font-mono text-xs font-bold`; plate `rounded-xl bg-ink/5 border border-line` with rivets from `::before/::after`; lock glyph from two pseudo-elements | **Must pass the policies markup nets:** section tags `section div p span h2 dl dt dd strong form input button select option label fieldset legend a` and attributes `class method action href type name value maxlength required` only; the guarantee block only `section div p span h2` with `class` only and no touch-target class. So: **no `<svg>`, `<ol>/<li>`, `<h3>`, `id`, `for`, `aria-*`, `role`, `<details>`** on `/admin/policies`. Numbers from the engine's `Order`; baseline / tenant rows unnumbered. |
| **Switch** | new `.rule-switch`, `--on/--off`, `.rule-switch-knob` (register) | `<form method=post>` + hidden `op`, `policy` + `<button type=submit class="rule-switch rule-switch--on">` with the knob span and the **outcome label** ("Switch this rule off" / "Switch this rule back on"); `inline-flex min-h-11 items-center gap-2.5 rounded-[22px] pl-1.5 pr-3.5 text-[13px] font-bold` | no `role="switch"`, no `aria-pressed`; state shown by the state chip; absent (not disabled) when there is no control |
| **Diff before / after** | new | before `rounded-[14px] border border-line bg-porcelain p-4`; after `rounded-[14px] border-2 border-ink bg-paper p-4`; pills `<del>` / `<ins>` with effect tones | grid `md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]`, arrow `hidden md:flex`. Only op-truthful diffs (switch off/on, author new/supersede, bind/unbind) — never an effect change. |
| **Key row** | new (list form of the keys table) | `flex items-center justify-between gap-3 border-t border-dashed border-line py-3`; name `font-display text-[15px] font-bold`; meta `font-mono text-xs text-ink/70` | table `hidden desk:table`, list `desk:hidden`. "Starts with" = `PrefixHint` + "…" (first characters, not last). |
| **One-time reveal box** | new `components.OneTimeSecret` | `flex flex-col gap-2.5 rounded-[14px] border-2 border-saffron bg-saffron/10 px-[18px] py-4`, `role="status"`; readonly mono input `min-h-11 rounded-[10px] select-all`; "I've stored it" = plain link | the value appears **exactly once** in the response (no title, `data-`, hidden copy); no Copy button |
| **Delivery status pill** | new (status pill tones) | `inline-flex rounded-md border px-2 py-0.5 font-mono text-[11px] font-bold tracking-[0.08em] text-ink` + delivered green / retrying dashed saffron / queued `border-ink/50` / sending dashed `border-ink/50` / failed tomato | **5 states** incl. Sending; tone from a literal templ switch |
| **Step progress** | new | dot `inline-flex size-9 items-center justify-center rounded-full font-mono text-xs font-bold`: current `bg-ink text-paper`, done `bg-tappa-green text-paper`, next `border border-ink/50 bg-paper`; connector `w-0.5 min-h-[26px] bg-line`; static `<ol>` with `aria-current="step"` | Reset (3 steps), Signup (3 steps + done; APPROVED stamp when done), Manual entry ("1 Fill in · 2 Check" only). Phone: "Step N of 3" + compact dots / progressbar. Never clickable. |
| **Split auth layout** | restyles `AdminLogin` inside `layout.Auth` (shell bytes unchanged) | `flex min-h-screen flex-col desk:flex-row`; brand half `bg-ink text-paper desk:flex-[1_1_520px]`; form half `desk:flex-[1_1_560px]`; card `rounded-[18px] border border-line bg-paper px-9 pt-9 pb-8` | phone: brand band + plain form; tablet: band on top; decorative dockets `aria-hidden`, tilt with literal `-rotate-2` classes; decorative `h-1 bg-tappa-green` bar allowed, never `.panel-stripe` / `bg-brand` |
| **Empty state** | new `components.PanelEmpty(where, heading, cta)` (the old `components.EmptyState` / `EmptyStateLabeled` lost their last caller to it and were removed in M12-48) | `flex flex-col gap-2 rounded-[18px] border border-dashed border-ink/50 p-6`; eyebrow = section name (`.lbl`); **h2** `text-[19px]` (as built — this row said h3; the heading sits directly under the shell's h1, so h3 would skip a level); body; optional CTA link | keep every existing heading/body variant and the `Queried` gate; callers re-pointed in M12-09 |
| **Notice** | new `components.PanelNotice(tone, heading)` (do **not** edit `components.Notice`) | ok `rounded-[14px] border border-tappa-green bg-tappa-green/10 px-[18px] py-4` + label "Done"; warn `border-saffron bg-saffron-lite` + "Heads up"; alert `border-2 border-tomato bg-paper` + "Not done" | `role="alert"` for alert, `role="status"` otherwise; label word first ("Done" in tappa-green on green/10 = 6.63:1) |
| **Problem** | new `pages.PanelProblem(PanelProblemView{Status, OwnerOnly, Title, Message, Hint, CTAHref, CTALabel})`; `AdminAuth.renderProblem` switches to it (`pages.Problem` stays for tap/activation) | card `rounded-[18px] border border-line bg-paper p-6 flex flex-col gap-3 max-w-xl`; code chip `rounded-md border border-ink px-2 py-0.5 font-mono text-xs font-bold tracking-[0.1em]`; owner pill `rounded-md bg-ink px-2 font-mono text-[11px] text-paper` | code from the status renderProblem already receives; unauthenticated problems link to `/admin/login`, never "Back to The Pass"; no 401 page; per-problem copy stays |

### 5.1 As built — the names the port shipped, and its recorded deviations (M12-48)

The table above is the handoff as written; this is what the code calls things after M12-01 …
M12-47. Where the two differ, the code is right (D1) and the reason is here.

| Component | Shipped as | Where |
|---|---|---|
| Shell heading slots | `PanelChrome.Kicker`, `.Title`, `.Crumbs []Crumb{Label, Href}`; actions through the wrapper `@pages.PanelActions(x) { @PanelShell(…) { … } }` — not a `PanelChrome` field (a `templ.Component` field broke the §4.7 type walks) | `pages/adminview.go`, `pages/admin.templ` |
| Chip / segmented | `.chip-link`, `.chip-link--on`, `.chip-count`, `.chip-group` | `input.css` |
| Form field | `.field-stack`, `.field-input`, `.field-input--mono`, `.field-help` | `input.css` |
| Ladder | `.ladder`, `.rung`, `.rung-no`, `.plate`, `.lock-glyph` (`.lock-mini` small) | `input.css` |
| Switch | `.rule-switch`, `--on`, `--off`, `.rule-switch-knob` | `input.css` |
| Tally as a pill | `.pill-tally` beside a `.tally--*` tone | `input.css` |
| Status pill | `.pill`, `.pill--active`, `.pill--invited`, `.pill--deactivated` (database words) | `input.css`, `components/roster.templ` |
| Mono pill (key, webhook, delivery) | `.mono-pill` + a tone written beside it in the template | `input.css`, `pages/apikeys.templ` |
| Stat card | `.stat-card`, `--spike`, `--open` | `input.css` |
| Evidence tile / badge | `.ev-tile` (`--ok/--bad/--none`, `-mark`, `-value`, `-sub`), `.ev-badge` (`--ok/--none/--wide`) | `input.css` |
| Queue item, pager | `.queue-item`, `--current`; `.spike-step`, `--off`; decision row `.spike-actions` | `input.css`, `components.SpikeQueueEntry` |
| Roster row | `.roster-row`, `.roster-initials`, `.roster-chevron` | `input.css`, `components.RosterRow` |
| Venue card | `.venue-card`, `--on`; `components.VenueCard`, `VenueDetail` | `components/venuedetail.templ` |
| The Roll | `.roll-table`, `.roll-row`, `.roll-who`, `.roll-name`, `.roll-sub`, `.roll-day`, `.roll-letter`, `.roll-cell` (`--worked/--other/--manual/--open/--none`), `.roll-mark`, `.roll-sum`, `.roll-shifts`, `.roll-body` | `input.css`, `pages/reports.templ` |
| Odd shapes | `.odd-card`, `.odd-title`, `.odd-chip`, `--open`, `.odd-callout`, `--alert`, `--warn` | `input.css`, `pages/anomalies.templ` |
| Manual entry | `.manual-dircard`, `.manual-actbar` | `input.css`, `pages/manualentry.templ` |
| Rule change diff | `.diff-del` / `.diff-ins` — arrive with M12-38 (`pages/policychange.templ`) | M12-38 branch |
| Stamp | `components.VerdictStamp(verdict)` | `components/docket.templ` |
| Rail docket | `components.RailDocket(DocketView)`; tilt keyed on `.rail-item … article` | `components/docket.templ`, `input.css` |
| Notice / empty / problem | `components.PanelNotice(tone, heading)`, `components.PanelEmpty(where, heading, PanelEmptyCTA{Label, Href})`, `pages.PanelProblem(PanelProblemView)` | `components/panelstates.templ`, `pages/panelproblem.templ` |
| One-time reveal | `components.OneTimeSecret(heading, fieldID, label, secret, doneHref)` | `components/onetimesecret.templ` |
| Plaque drawing | `components.PlaqueDrawing(PlaqueDrawingView)`, `components.PlaqueNotOnAWall()`; QR from `(*qrcode.Code).SVGPath`, URL from `sun.PlaqueQRURL` | `components/plaquedrawing.templ` |
| Bucket helpers | `components.WidthPct`, `PctOf`, `SplitPct`, `TapeBar` | `components/buckets.templ` |

**Recorded deviations.**

- **PanelEmpty's heading is an `h2`**, not the `h3` the catalogue row said: it sits directly under
  the shell's one `<h1>`.
- **Docket padding recipe not applied** (M12-27): the full docket keeps `.docket`'s own padding,
  because `TestTransactionDocket_IsADocketAndCarriesEveryFieldTheCardAsksFor` pins its class
  attribute as exactly `class="docket"` and the class is shared with the employee result screen
  (§3.5). See the Docket row above.
- **M-More rows are kept as code** (M12-47, screen row S8): the current board (`fd18724` /
  `c2dd88d`) draws the shipped flat rows (`.sheet-list`, at least 48 px, a plain bottom rule), not
  the first full-page More's rounded grouped list with chevrons and dashed separators (`0e31694`).
  The polish is CSS only: the lift shadow, the scrim over the page and the sign-out radius.
- **Odd shapes at the desk width uses the tablet arrangement** (M12-29), one DOM: the desk board's
  own grid does not hold in the 864 px box and one markup cannot carry both column assignments
  ([spike-odd O.3](screens/spike-odd.md#o3-breakpoints)).
- **The Pass's phone h1 reads "The Pass · <day>"** (M12-27): the Title is one string printed once;
  spec L2 asked for the day alone on the phone ([pass-ledger L2](screens/pass-ledger.md)).

**Lessons the port paid for** (each one red at least once):

- **No two-decimal arbitrary value in markup** (e.g. a line height of 1.45 written as an arbitrary
  value): the coordinate sweep (`decimalDegreeShape`,
  `TestAnomaliesPages_CarryNoDecimalDegreeInEITHEROfTheOtherTwOShapes`) reads it as a decimal
  degree. Use a named step (`leading-normal`, `leading-snug`) or a component class.
- **templ drops a literal leading space of text inside an element**: write `{ " " + x }` (or
  `{ " " }` before the expression) where a space must separate two pieces.
- **Tailwind's scan is lexical** (§3.2): a class name that is an ordinary word compiles from prose.
- **`.tally*` selectors are reserved for tones** (`TestTallyRules_NoToneRuleColoursTheWord`): a
  tally tone colours the frame and the ground, never the word; layout on a tally goes in its own
  class (`.pill-tally`).


### Bucket helpers (literal classes, one `.templ` file)

- **Width** — `components.WidthPct(p int) string` returns one of 21 literals: `w-0`, `w-[5%]`,
  `w-[10%]`, …, `w-[95%]`, `w-full`. Integer percent to the nearest 5, held to 0..100;
  `components.PctOf(n, of int) int` is the zero-safe `n*100/of`; stacked bars use
  `components.SplitPct(counts []int) []int` (largest remainder in steps of 5, sums to 100; all 0
  when nothing is counted). Bars `h-full shrink-0`; tracks `overflow-hidden rounded`.
- **Tape height** — `components.TapeBar(bucket int) string` returns `tape-bar--1` … `tape-bar--10`
  (`""` — no bar — for 0 or less, the tallest above 10). All four live in
  `web/templates/components/buckets.templ` (exported: the screens are in `pages`).
- **Tilt** — `nth-child` rules in `input.css` (rail, Choose dockets); never a per-item class built in
  Go.

---

## 6. Screen map

| Boards (desk · tablet · phone) | Route | Handler | Templ | View model | Card |
|---|---|---|---|---|---|
| `Main` · `T-Pass` · `M-Pass` | `GET /admin` (no `date`) | `(*AdminAuth).transactionsSection` (`internal/handler/transactions.go:104-144`) | `pages/transactions.templ` `AdminTransactions`; `components/docket.templ` | `pages.TransactionsView`, `components.DocketView` | M12-26 |
| `Ledger` · `T-Ledger` · `M-Ledger` | `GET /admin?date=…` (+ `GET /admin/dockets` fragment) | same; `transactionDockets` | same + `components/filterbar.templ` | `TransactionsView`, `FilterBarView` | M12-27 |
| `Spike` · `T-Spike` · `M-Spike` | `GET`/`POST /admin/review` | `reviewSection` / `reviewDecision` (`internal/handler/review.go`) | `pages/review.templ` `AdminReview` | `pages.ReviewView`, `ReviewItem` | M12-28 |
| `Odd` · `T-Odd` · `M-Odd` | `GET /admin/anomalies` | `anomaliesSection` (`internal/handler/anomalies.go`) | `pages/anomalies.templ` | `pages.AnomaliesView` | M12-29 |
| `People` · `T-People` · `M-People` | `GET /admin/employees` (`?manage=`) | `employeesSection` (`internal/handler/employees.go:155`) | `pages/employees.templ`, `components/roster.templ` | `EmployeesView`, `RosterRowView`, `RosterActionsView` | M12-30 |
| `StaffForm` · `T-StaffForm` · `M-StaffForm` | `GET /admin/employees?add=new`, `?manage=<id>`; `POST /admin/employees/{add,invite,deactivate,move,email,external-ref}` | `employeeAdd` and siblings (`internal/handler/employeeactions.go`) | `components/staffform.templ`, `components/roster.templ` | `RosterAddView`, `StaffFormView`, `RosterActionsView` | M12-31 |
| `Invite` · `T-Invite` · `M-Invite` | `POST /admin/employees/invite` | `employeeInvite`, `employeeInviteByEmail` | `pages/employees.templ` `AdminInviteIssued` / `AdminInviteEmailed` | `InviteIssuedView`, `InviteEmailedView` | M12-32 |
| `Manual` · `T-Manual` · `M-Manual` | `GET`/`POST /admin/records/new`, `POST /admin/records` | `manualEntryForm` / `manualEntryReview` / `manualEntryRecord` (`internal/handler/manualentry.go`) | `pages/manualentry.templ` | `pages.ManualEntryView` | M12-33 |
| `Venues` · `T-Venues` · `M-Venues` | `GET /admin/locations` (`?at=`, `?plaque=`) | `locationsSection` (`internal/handler/locations.go:229-335`) | `pages/locations.templ`, `components/venue.templ`, `components/plaque.templ` | `LocationsView`, `VenueRowView`, `PlaqueRowView` | M12-34 (QR: M12-12) |
| `VenueForm` · `T-VenueForm` · `M-VenueForm` | `GET /admin/locations?venue=…` / `?department=…`; venue/department POSTs | `venueForms`, `saveVenue`, `saveDepartment` | `components/venue.templ` `VenueForm`, `DepartmentForm`, `Removal` | `VenueFormView`, `DepartmentFormView`, `RemovalView` | M12-35 |
| `Week` · `T-Roll` · `M-Roll` | `GET /admin/reports?week=…` (CSV `GET /admin/reports.csv`) | `reportsSection`, `fillReportsView` (`internal/handler/reports.go`) | `pages/reports.templ` | `pages.ReportsView` | M12-36 |
| `Rules` · `T-Rules` · `M-Rules` | `GET /admin/policies` | `policiesSection` (`internal/handler/policies.go:98-133`) | `pages/policies.templ` | `pages.PoliciesView` | M12-37 |
| `PolicyChange` · `T-PolicyChange` · `M-PolicyChange` | `POST /admin/policies/change` (→ `/apply`), `GET /admin/policies/version` | `policyChangeReview`, `policyVersionPage` (`internal/handler/policyactions.go`) | `pages/policychange.templ` | `PolicyChangeView`, `PolicyVersionPageView` | M12-38 |
| `Billing` · `T-Billing` · `M-Billing` | `GET /admin/billing` (CSV `GET /admin/billing.csv`) | `billingSection` (`internal/handler/billing.go:161-197`) | `pages/billing.templ` | `pages.BillingView` | M12-39 (bug: M12-02) |
| `FreezeMonth` · `T-FreezeMonth` · `M-FreezeMonth` | `POST /admin/billing/close` (→ `/apply`) | `billingCloseReview` (`internal/handler/billingactions.go:179-240`) | `pages/billing.templ` `AdminBillingClose` | `BillingCloseView` | M12-40 |
| `Account` · `T-Account` · `M-Account` | `GET /admin/account` (+ password and brand POSTs) | `accountSection` (`internal/handler/account.go:157`) | `pages/account.templ` | `AccountView`, `BrandEditor` | M12-41 |
| `Developers` · `T-Developers` · `M-Developers` | `GET /admin/api-keys` (+ key and webhook POSTs) | `apiKeysSection` (`internal/handler/apikeys.go:123`), `readWebhookView` | `pages/apikeys.templ` | `APIKeysView`, `WebhookView` | M12-42 |
| `States` · `T-States` · `M-States` | reference board: every `EmptyState` caller + `renderProblem` | `(*AdminAuth).renderProblem` (`internal/handler/adminlogin.go:1757`) | `components/panel.templ` `EmptyState` → new `PanelEmpty`, `PanelNotice`, `PanelProblem` | — | M12-09, M12-10 |
| `SignIn` · `T-SignIn` · `M-SignIn` | `GET`/`POST /admin/login` | `renderLoginScreen` | `pages/admin.templ` `AdminLogin` | `AdminLoginView` | M12-43 (copy: M12-04) |
| `Choose` · `T-Choose` · `M-Choose` | `GET`/`POST /admin/login/choose` | choose handlers (`adminlogin.go:526-527`) | `pages/admin.templ` `AdminChoose` | `AdminChooseView` | M12-44 |
| `Reset` · `T-Reset` · `M-Reset` | `/admin/reset`, `/admin/reset/new` | `internal/handler/adminreset.go` | `pages/adminreset.templ` | `AdminResetRequestView`, `AdminResetSentView`, `AdminResetNewView` | M12-44 (copy: M12-05) |
| `Signup` · `T-Signup` · `M-Signup` | `/signup`, `/signup/places`, `/signup/account`, `/signup/done` | `internal/handler/signup.go` | `pages/signup.templ` | `Signup*View` | M12-45 (copy: M12-06, M12-07) |
| `Legal` · `T-Legal` · `M-Legal` | `/legal/{privacy,terms,imprint,cookies}` — **public**, marketing chrome | `legal` (`internal/handler/marketing.go:337-349`) | `pages/legal.templ` | `LegalPageView` | M12-46 |
| `M-More` | shell's `<details>` sheet | — | `components/panel.templ` `BarNav` | — | M12-47 |
| shell (all boards) | every panel section | `a.chrome` | `pages/admin.templ` `panelChrome` | `pages.PanelChrome` | M12-01 |
</content>
</invoke>
