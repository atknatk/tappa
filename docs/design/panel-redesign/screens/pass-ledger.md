# The Pass (today) and the past day / ledger — `/admin`

Boards: `Main` · `T-Pass` · `M-Pass` (today) and `Ledger` · `T-Ledger` · `M-Ledger` (past day).
Every citation refers to **HEAD `b284899`**. Decisions D1–D4 and the inherited constraints:
[README](../README.md). The shell is done and not re-specified.

**Status words used in every screen file:**
**EXISTS** already on the view model · **DERIVE** computable from data the handler already loads
(no new SQL) · **REUSE** an existing query/domain read not yet called here · **BUILT (D3, M12-xx)**
a new read built under D3 in the named card · **KEPT AS CODE** the board differs and the code's
behaviour/wording stays (D1) · **REMOVED (D1)** drawn but not built — listed under "Not ported"
and in `docs/backlog.md`.

## 0. Facts to know first

1. **The day is a calendar day.** `ledger.page` builds `from = midnight(day, zone)`, `to = from + 1
   day` (`internal/domain/ledger/ledger.go:631-648`); `db/queries/tenants.sql:25-36` argues it ("a
   02:00 check-out … belongs to the next day's page"). "Shift day 06:00 → 06:00" and "same shift"
   are **REMOVED (D1)**; the tape axis is 00 → 24.
2. **CSP forbids inline styles** (`adminCSP`, `internal/handler/adminlogin.go:1782`): rail tilt via
   `nth-child`, tape heights via buckets.
3. **Tailwind scans only templ and `web/static/js`** — no class names built in Go.
4. **`/admin` is the one scripted panel page** (htmx, `pages.PanelShellWithScript`,
   `transactions.templ:51-52`; `a.renderScripted`, `internal/handler/transactions.go:143`). No new
   script, no `hx-on`, no `js:`, no filters. No live refresh (D1).
5. **Heading** via the M12-01 slots: Kicker (date · time · zone), Title ("The Pass" or "The Pass ·
   Mon 19 Oct"), Actions. One `<h1>`.
6. **Mono 500 does not exist** → `font-normal`.
7. **Never 0 for unmeasured.** Every widget is tri-state (`PendingBadge`, `adminview.go:415-460`;
   `PlaqueCounts.Queried`, `transactionsview.go:109-114`). A failed **widget** read says "could not be
   read" and does not fail the page; a failed **day** read still goes to the problem page
   (`transactions.go:108-113`).
8. **Today vs ledger layout** (resolved D7): `/admin` with no `date` renders The Pass (rail = the first
   page of 25, newest first, plus "See the whole day →" to `/admin?date=<today>`); with `date` it
   renders the ledger. Same handler, same reads.

---

## 1. The Pass (today) — `Main` / `T-Pass` / `M-Pass`

### 1.1 Wiring

| Item | Where |
|---|---|
| Route | `GET /admin` from `pages.PanelSections` (`TabTransactions`, `web/templates/pages/adminview.go:287`) in `mountSections` (`internal/handler/dashboard.go:60-62`); fragment `GET /admin/dockets` (`dashboard.go:89`, `transactions.go:76`) |
| Handler | `transactionsSection` (`internal/handler/transactions.go:104-144`); `transactionDockets` (`:172-186`); `fillTransactionsView` (`:193-233`), `docketView` (`:236-276`), `sign` (`:286-295`), `parseTransactionFilter` (`:307-331`), `filterQuery` (`:335-347`) |
| Domain | `ledger.Reader.Screen` (`internal/domain/ledger/ledger.go:469-~500`): `GetTenantClock` → `page` (`ListPanelTransactions`, `PageSize` 25, `:67`) → `nameSiteRecords` (`site.go:68-96`) → `options` → `advisories` (`CountTenantPlaques`, `TenantHasAnyTransaction`) |
| Templates | `web/templates/pages/transactions.templ` (`AdminTransactions` :51, `plaqueNotice` :137, `TransactionDockets` :202, `dockets` :207, `keyBudgetBanner` :253); `components/docket.templ` (`TransactionDocket` :32, `verdictStamp` :187); `components/filterbar.templ` (`FilterBar` :30); `components.EmptyState` (`panel.templ:278`) |
| View models | `pages.TransactionsView` (`transactionsview.go:17-66`); `components.DocketView` (`components/docketview.go:33-159`); `FilterBarView` (`:171-215`); `PlaqueCounts` (`transactionsview.go:109-134`); `KeyBudgetBanner` (`:234-240`); `PanelChrome.Pending` (`adminview.go:395-413`) |

### 1.2 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| P1 | Kicker "Tue 20 Oct · 14:32 · Europe/Malta" | `page.Day` + zone (`transactions.go:205-207`); render time `time.Now().In(zone)` | DERIVE | "as of page load" (page is `no-store`); **full zone name** (the zone must be visible, `transactionsview.go:36-40`); shift clause **REMOVED** |
| P2 | Title "The Pass" | shell | EXISTS | size via M12-01 |
| P3 | Header search | — | **REMOVED (D1)** | no venue/plaque search exists; the ledger's employee filter stays where it is |
| P4 | "Manual entry" button | `GET /admin/records/new` needs `?id=` (`manualentry.go:153-162`) | **REMOVED (D1)** | a manual entry starts from a person (People card → "Enter a record by hand") |
| P5 | "On the floor now" big number | latest open `in` per person within `tap.StaleOpenIn` (18 h, `internal/domain/tap/decide.go:481`) | **BUILT (D3, M12-13)** | counts flagged open check-ins too (they are records); tri-state |
| P6 | "of 41 rostered · 4 not in yet" | — | **REMOVED (D1)** | no roster concept (`00002…sql:32-38,79-83`, `00003…sql:32-40`) |
| P7 | Per-venue tally | same read, grouped by `coalesce(location_id, site_location_id)`; partner names via `siteVenuesFor` (`site.go:47-64`) | **BUILT (D3, M12-13)** | real location names (no "KF " stripping); ticks capped at 20, the number carries the meaning; **no denominator, no grey gap ticks** |
| P8 | "On the spike" card | `PanelChrome.Pending` (`review.go:540-553`, `reviews.sql:55-80`) | EXISTS | `Label()` / `Describe()`; "0" only when `Known && N == 0`; all days' flags — copy never says "today" |
| P9 | "Left open since yesterday" card | `ListOpenCheckIns` (`anomalies.sql:476`), yesterday's calendar day in the tenant zone, capped | **BUILT (D3, M12-15)** | "N+" when capped; link to Odd shapes; "Not counted in hours until a manager closes them." (closing = a new manual record) |
| P10 | "The rail · newest on the left" | — | DERIVE | "· live" **REMOVED (D1)** |
| P11 | Rail chips All / Approved / Flagged / Rejected / Ignored | `verdict` param + `panelVerdicts` (`transactions.go:89-101`, `oneOf` `:424-431`) | EXISTS (as links) | `<a href="/admin?verdict=flag">` with `aria-current`; **all four** values the validator accepts; "All" = no param; `min-h-11`; `rounded-full` (D2) |
| P12 | Rail track + clip + tilted dockets | `v.Dockets` (first page, newest first) | EXISTS (data) | tilt by `nth-child`; rail ends with "See the whole day →" and keeps "Show more" reachable |
| P13 | Compact docket | `DocketView.Venue/Verdict/Who/Direction/At/Trust/IPSign/GPSSign` | EXISTS | `RailDocket` (M12-11). Keep K-39 ("not shown at a partner venue" instead of trust + signs, `docket.templ:78-86`), the manual tally or MANUAL mark, "Nobody recognised" / "No location recorded" (`:38-54`), "none" for empty direction/trust, TRAINING stamp. Signs **KEPT AS CODE** (words "IP yes · GPS no" / "n/a"). Board's note text in the evidence slot and "TRUST 0" are sample data. |
| P14 | The tape | people on shift per venue per hour, elapsed hours only | **BUILT (D3, M12-14)** | done `bg-tappa-green`, current hour `bg-ink`; "rostered, still ahead" bars **REMOVED (D1)**; "runs past midnight" **REMOVED**; capped read says the tape is incomplete; accessible table / `aria-label` per row; partner-venue rows via `siteVenuesFor` |
| P15 | Plaques card | `tenant.Plaques.Screen` (`internal/domain/tenant/plaque.go:663-~700`; `ListTagsForTenant` `tags.sql:91-112`, `ListTagLastSeen` `:261-337`, `ListSiteTagLastSeen`, `mergeLastSeen` `plaque.go:1607`); `a.plaques` already on `AdminAuth` (`plaques.go:138`) | REUSE | mounted (`active`) plaques, newest last-seen first, cap 8 + link to Venues & plaques. Row: venue name (join `LocationID` to the options `ledger.Screen` loads, `ledger.go:764-784`) · `PLAQUE 91AC` (last 4 of the uid) · "ago" in the zone, **"never"** for a plaque with no last-seen row (`tags.sql:280-287`) · state word from the Venues vocabulary ("In service", "Needs replacing" when the key alert is `wall`). Position ("· front") and "REPLACE" **REMOVED (D1)**. Failure → one line "could not be read". |
| P16 | Plaque notice / key-budget banner | `plaqueNotice`, `keyBudgetBanner` (`transactions.templ:60`, `:137-183`, `:253-275`) | EXISTS — **keep** | above "Right now"; the banner's absence adds 0 bytes (`TestKeyBudgetBanner_AbsentAddsNothing`) |
| P17 | Empty rail | `v.Queried`, `emptyHeading`, `OffersAnotherDay` (`transactions.templ:66-105`, `:243-248`) | EXISTS — **keep** | "No taps recorded on this day" / "Nothing was recorded here on this day." — gated on `Queried` |
| P18 | "Show more" | `MoreHref` / `MoreFragmentHref` (`transactionsview.go:42-50`) | EXISTS — **keep** | §4.6 |
| P19 | Phone top bar tenant name | shell | KEPT AS CODE | shell shows the wordmark unless branded |

### 1.3 Breakpoints

| Block | Phone | Tablet `md` | Desktop `desk` |
|---|---|---|---|
| Header | kicker "date · time · zone"; h1 30 px | kicker; h1 36 px | kicker; h1 44 px |
| Right now | floor card full width (`flex-col`, number 72 px, tally below); spike + open in 2 columns (number 48 px) | floor card full width (`flex-row items-end gap-6`, number 80 px); spike + open 2 columns (56 px) | 4-column grid, floor spans 2 (88 px, `flex-wrap gap-7`); spike, open (64 px) |
| Rail | heading; chips in a scroller (`-mx-4 px-4`, `flex-none`); dockets 190 px | chips wrap below the heading; dockets 200 px; scroller bleed `-mx-8 px-8` | heading + chips one row (`flex-wrap justify-between items-baseline`); dockets 214 px |
| Tape | own card; inner `min-w-[560px]` in a scroller; label col 74 px; bar 28 px | `min-w-[640px]`; label col 88 px; bar 34 px | tape (`flex-[999_1_560px]`) and plaques (`flex-[1_1_300px]`) in one `flex-wrap` row; label col 96 px (fits 864) |
| Plaques | single column | `grid-cols-2 gap-x-6` | single column beside the tape |

### 1.4 Recipes

Stat card, big number, chips, rail + clip, rail docket, tally ticks, tape bars, plaque list row:
README §5. "Right now" grid `section.grid grid-cols-2 gap-3 md:gap-3.5 desk:grid-cols-4 desk:gap-4`.
Plaque list row `li.grid grid-cols-[1fr_auto] gap-x-2.5 gap-y-0.5 border-b border-dashed border-line
py-2.5`; state via `.tally` + `tally--mounted` / `tally--unencoded`. Eyebrow: reuse `.docket-label`
or one `.lbl` class (its grounds paper, porcelain, saffron-lite all pass at /70).

### 1.5 Not ported (The Pass)

header search and its "/" hint · "Manual entry" button (top bar and header) · "of N rostered",
"not in yet", tally denominators and grey gap ticks · "shift day 06:00 → 06:00" · "live" ·
"rostered, still ahead" tape bars and legend · "the tape runs past midnight" · plaque positions
("front", "back door") · "REPLACE" status · sample notes in the evidence slot.

### 1.6 Binding copy (The Pass)

| Board | Code | Port |
|---|---|---|
| "Tue 20 Oct · 14:32 Malta · shift day …" | "Monday 5 August 2026 · Europe/Malta" (`transactions.go:206-207`) | short date + time + **full zone**; no shift clause |
| "On the spike" / "taps the rules couldn't judge alone" | badge `Describe()` "N records waiting for review" (`adminview.go:449-460`) | new copy OK; never "today" |
| "Left open since yesterday" / "Not counted in hours until a manager closes them." | Odd shapes definition (`anomalies.sql:477-485`) | OK (§5 "Unutulan çıkış") |
| Chips | `panelVerdicts` labels Approved / Flagged / Rejected / Ignored (`transactions.go:90-95`) | the slice's labels; add Ignored |
| "TRUST 100" "IP ✓ GPS ✓" | "Trust" / "IP yes · GPS no" / "n/a" (`docket.templ:89-100`, `sign` `transactions.go:286-295`) | **code words** |
| "Plaques" / "OK" / "REPLACE" | Venues vocabulary | "In service", "Needs replacing" (key alert), "never" |

### 1.7 Tests

| Test | File | Keep |
|---|---|---|
| `TestKeyBudgetBanner_AbsentAddsNothing` + goldens `testdata/key-budget-golden/transactions-home-{zero,plaques}.golden` | `internal/handler/panelbrand_test.go:985-1014` | goldens change → dedicated re-render commit with the byte-check (absent banner = 0 bytes; banner drawn once); keep `templ.Join(keyBudgetBanner, plaqueNotice)` |
| `TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty` | `internal/handler/dashboard_test.go:563` | `/admin` stays the only scripted page |
| `TestPanelMarkup_UsesNoHtmxSyntaxThatNeedsUnsafeEval`, `TestDocketFragment_UsesTheUnwidenedPolicy`, `TestDocketFragment_IsBehindTheSessionGate`, `TestVendoredScript_MatchesTheRecordedDigest` | `transactions_test.go:156,1004,831,1113` | unchanged behaviour |
| `TestTypes_NothingOnTheReadPathCanCarryACoordinate` | `transactions_test.go:78` | no new field starting `ip…`, `gps…`, `lat…` |
| `TestTransactions_AFailedReadIsAnErrorAndNeverAnEmptyDay`, `TestTransactions_AnUnqueriedViewClaimsNothing` | `transactions_test.go:299,327` | each new widget gets its own "unqueried claims nothing" case |
| `TestTransactionDocket_*`, `TestPolicyNote_IsEscapedOnBothSurfacesThatPrintIt` | `transactions_test.go:598,654,698,1188,1255` | "IP yes"/"GPS yes", `class="docket"`, no `<table`, "APPROVED", "641", manual + TRAINING distinguishable |
| `TestTransactionDocket_ShowsTheEngineVerdictAndTheManagerDecisionSeparately` | `review_test.go:1136` | stamp + manager tally, never swapped |
| `TestPanelScreens_FormControlsCarryATouchTargetClass` | `transactions_test.go:786` (+ `panelTouchTargets`, `dashboard_test.go:915`) | chips registered |
| `TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason` | `holdingpanel_db_test.go:33` (`:112-119`) | partner rail docket: venue name + "not shown at a partner venue"; the negative "IP no · GPS yes" assertion stays meaningful (signs unchanged) |
| `TestEmployerReaders_NineReadersNameTheSite`, `TestEmployerReaders_APartnerVenueRowCarriesNoEvidenceSentence` | `internal/domain/ledger/holding_db_test.go:243,725` | add the floor tally, tape and left-open readers as cases, both directions |
| `TestEvidenceReaders_EveryOneIsClassified` | `internal/db/evidencereaders_test.go:118` | file every new statement that reads an evidence column |
| `TestEmployeeLocationReaders_Counted` | `internal/db/m11schema_test.go:1035` | classify any new statement naming `employees` + `location_id` |
| `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` | brand tests | no new selector reads `--brand-*` |
| fakes behind `panelLedger` | `transactions.go:56-68` | extend fakes in the same change |

### 1.8 Red lines (The Pass)

§4.3 read-only; closing an open check-in is the existing manual-entry flow · §4.5 every new
statement repeats `tenant_id` in WHERE and every JOIN's ON (`transactions.sql:452-456` style);
partner names only via `holding_site_locations` · §4.6 no invented numbers; capped reads say so ·
§4.7 signs only, no coordinate/distance · §4.1/§4.2 "on the floor" is derived from recorded taps,
not tracking; no "live" wording · K-39 no trust/IP/GPS/evidence note on partner rows; partner rows
never in an evidence count · performance: the home page grows from 1 read to ~5 — one `WithTenant`
transaction with a savepoint per widget (`inSavepoint`, `ledger.go:615-629`), EXPLAIN recorded.

---

## 2. Past day / ledger — `Ledger` / `T-Ledger` / `M-Ledger`

### 2.1 Wiring

As §1.1: `GET /admin?date=YYYY-MM-DD&location=&department=&employee=&verdict=&channel=` plus cursor
`after_at` / `after_id` (`transactions.go:307-331`); fragment `GET /admin/dockets` serves "Show more".

### 2.2 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| L1 | Kicker "Europe/Malta · records are never edited" | `ZoneLabel` (`transactionsview.go:40`) | EXISTS / DERIVE | "records are never edited" is true (§4.3); shift clause **REMOVED** |
| L2 | Title "The Pass · Mon 19 Oct" (phone "Mon 19 Oct") | shell label + `page.Day` (`Format("Mon 2 Jan")`) | DERIVE | via the Title slot; one `<h1>` |
| L3 | Day stepper ◀ ▶ | `page.Day` ± 1, `filterQuery(f, day)` (`transactions.go:335-347`) | DERIVE | two `<a>` (`aria-label` "Previous day" / "Next day"); next from today hidden or links to today; cursor params never carried (`filterbar.templ:21-24`) |
| L4 | "Today" | `/admin` | DERIVE | "live" **REMOVED** |
| L5 | Filter form | `components.FilterBar` (`filterbar.templ:30-71`), `FilterBarView` | EXISTS (restyle) | verdict moves to chips → hidden `verdict` input keeps it on Apply; "Clear" only when `Narrowed` (`filterbar.templ:63-68`, kept); channel labels from `panelChannels` ("Plaque tap (NFC)", "QR scan", "Entered by a manager", `transactions.go:96-100`); phone hides Day (hidden `date` input); every control keeps `filter-input` |
| L6 | Summary "10 records · 6 approved · … · 1 manual entry" | — | **BUILT (D3, M12-16)** | same predicates as `ListPanelTransactions` minus verdict and cursor, in the same `WithTenant`; pluralised; tri-state (no line when not measured) |
| L7 | Verdict chips with counts | `verdict` param + L6 | EXISTS + BUILT | links keeping every other filter; phone scroller |
| L8 | Empty state | `emptyHeading` + bodies (`transactions.templ:66-105, 243-248`) | KEPT AS CODE | "No records match these filters" for narrowed; "No taps recorded on this day" otherwise; `OffersAnotherDay` gate; "Pick another day above" may become "Pick another day" |
| L9 | Hour groups | `rec.OccurredAt.In(zone)` truncated (`transactions.go:242`) | DERIVE | **descending** hours (newest first, D1); "same shift" sub-label **REMOVED**; weekday sub-label DERIVE; add an hour key to the view; a "Show more" page that starts mid-hour repeats the header with "(continued)" (no extra URL param) |
| L10 | Full docket | `components.TransactionDocket` (`docket.templ:32-176`) — already this layout | EXISTS | label "Direction" (code); keep Department (`:44-46`); ctr **unpadded** as the code prints it (`#612`, `transactions.go:273`, `docket.templ:107-108`; D1 — the board's `#000612` was corrected); "· partner venue" suffix from `PartnerVenue`; keep "Your own policy rule"; board notes are sample data |
| L11 | Docket grid | — | layout | §2.3 |
| L12 | Compact docket (tablet + phone) | `DocketView` | EXISTS (responsive parts) | **never merge the manager's note into the engine note** (`docket.templ:165-175`); partner phrase "not shown at a partner venue"; keep the manual tally |
| L13 | "Show more" | `MoreHref` / `MoreFragmentHref` | EXISTS — **keep** | |
| L14 | Plaque notice / key-budget banner | as P16 | EXISTS — **keep** | on every day |
| L15 | Phone `<details>` "Filters" | — | DERIVE | open by default when `Narrowed`; summary `min-h-12`; one form (no duplicate ids) — `.filter-details > summary { display: none }` from `md` and the form always shown there |

### 2.3 Breakpoints

| Block | Phone | Tablet `md` | Desktop `desk` |
|---|---|---|---|
| Header | h1 = day only; stepper `flex-1 justify-between` + "Today" | h1 "The Pass · day"; stepper + "Today" | stepper inline + "Today" right (`flex-wrap justify-between items-end`) |
| Filters | inside `<details>`; stacked; Apply full width 48 px | `grid grid-cols-3 gap-3 p-4` | `grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-3.5 px-5 py-[18px] items-end` |
| Summary + chips | summary `text-xs`; chips scroller | stacked | one row `flex-wrap justify-between items-center` |
| Hour group | header `flex items-baseline gap-2 border-t-2 border-ink pt-1.5`; cards one column | cards `grid grid-cols-2 gap-x-3.5 gap-y-5` | `grid grid-cols-[96px_minmax(0,1fr)] gap-4`; cards `grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-x-4 gap-y-[22px]` (2 columns at 864) |
| Docket | compact | compact | full (`dl` 4 columns) |

### 2.4 Recipes

Day stepper and "Today" (`.btn--ink`, `rounded-xl`): README §5. Filter panel: `.filter-bar` gains the
grid recipes + `rounded-[18px]`; `.filter-input` gets the soft look `rounded-[10px] min-h-11` (mono
kept for the date). Summary `p.font-mono text-xs md:text-[13px] text-ink/70`. Hour header: a
`<section aria-labelledby>` with the hour as `<h2>`, hour `font-mono text-lg md:text-xl
desk:text-[22px]`. Tallies reuse `.tally--manual / --queued / --approved / --rejected` (the board's
dashed `--queued` is not ported).

### 2.5 Not ported (ledger)

"Shift day 06:00 → 06:00" · "same shift" · "Today, live" · "Clear" shown when not narrowed ·
"NFC / QR / Manual" short channel labels · merged manager note · ascending order · sample notes
("212 m from the venue" etc.).

### 2.6 Binding copy (ledger)

| Board | Code | Port |
|---|---|---|
| Filter labels Day / Location / Department / Employee / Channel; "All locations", "All departments", "Part of a name", "Any channel", "Apply", "Clear" | identical (`filterbar.templ:33-67`) | keep |
| Channel options | "Plaque tap (NFC)", "QR scan", "Entered by a manager" | **code** (one slice for validator and options) |
| Empty | "No records match these filters" + "Nothing on this day matches those filters. Widen them, or clear them to see the whole day." / "No taps recorded on this day" + "Nothing was recorded here on this day. Pick another day above." (`transactions.templ:73-104`) | **code** |
| Docket labels | "Where" / "Who" / "Direction" / "Time" / "Trust" / "Evidence"; "Waiting for review" / "Approved by a manager" / "Entered by a manager" / "Manager's note"; "No plaque on this record" | **code** ("Dir" allowed on compact only) |
| Summary | — | "1 record", "2 manual entries" |

### 2.7 Tests (in addition to §1.7)

| Test | File | Keep |
|---|---|---|
| `TestTransactionFilters_ReachTheQuery`, `…_RefuseValuesTheDropdownNeverOffered`, `TestTransactionFilters_EveryOfferedOptionIsAccepted` | `transactions_test.go:363,413,457` | verdict offered as chips → the test reads the chip links / hidden input; every offered value round-trips, no unoffered value accepted |
| `TestTransactionPaging_CarriesTheFiltersAndTheCursor`, `…_LastPageOffersNothingToPress` | `transactions_test.go:507,569` | paging keeps every filter |
| `TestFilterBar_RendersNoEmployeeRoster`, `TestEmployeeFilter_*` | `transactions_test.go:889-1052` | employee stays a text box |
| `TestPanelTransactionsDB_*` | `internal/handler/transactions_db_test.go:155-640` | paging covers every record exactly once with grouping; the count query gets its own RLS test (no tenant filter in the test) and a "count equals list" test |
| `TestHoldinglessPanels_AreByteIdenticalToTheGolden` / `TestHoldinglessViews_AreByteIdenticalToTheGolden` | `web/templates/pages/holdingless_golden_test.go:64`, `internal/handler/holdingless_view_golden_test.go:78` | they cover reports and anomalies only; they break only if a shared component they render changes markup |

### 2.8 Red lines (ledger)

§4.3 read-only, "records are never edited" · §4.5 the count query repeats the tenant predicate; the
name CTE stays tenant-scoped and status-agnostic (people who left are found, `transactions.sql:481-485`)
· §4.6 "Show more" stays; counts and list from the same transaction · §4.7 no distance in notes · K-39
compact docket prints no Trust/Evidence labels on partner rows; partner verdict counts are residue
(the inventory entry says so).
</content>
</invoke>
