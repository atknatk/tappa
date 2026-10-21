# Venues & plaques, venue form, The Roll

Boards: `Venues` · `T-Venues` · `M-Venues`, `VenueForm` · `T-VenueForm` · `M-VenueForm`, `Week` ·
`T-Roll` · `M-Roll`. Citations refer to **HEAD `b284899`**. Decisions and constraints:
[README](../README.md). Status words: [pass-ledger](pass-ledger.md).

## 0. Cross-cutting

1. **Heading** via M12-01: Kicker ("8 venues · 11 plaques on walls", "Hours per person · week 42"),
   Actions (Add venue; week stepper + CSV), Title (VenueForm "Edit KF Valletta" becomes the Title,
   the section label then sits in the Crumbs).
2. **No inline style** (`adminCSP`, `adminlogin.go:1782`). Data-driven geometry (shift bar, QR modules)
   is **inline SVG with presentation attributes**, the pattern of `web/templates/operatorpages/qr.go`.
3. **No script** (`locations.templ:40-41`, `reports.templ:38-39`; `TestLocationsSection_LoadsNoScript`).
   Every board `onClick` becomes a link, a GET form, a POST form or `<details>`.
4. **Repeated chains become component classes** (`input.css:704-713`): The Roll has up to 100 rows ×
   7 cells → `.roll-cell*`.
5. **Contrast trap:** boards use `ink/50` as text (the "—" day, phone badges) — 3.23:1 on paper
   (`input.css:144-150`) fails AA. Text is `ink/70`.
6. **Colour is never the only carrier** (`reports.templ:33-36`).

---

## 1. Venues & plaques — `/admin/locations`

### 1.1 Wiring

| | |
|---|---|
| Route | `GET /admin/locations` (`locationsHref`) |
| Handler | `locationsSection` (`internal/handler/locations.go:229-335`); shell builder `locationsShell` (`locationactions.go:387-404`) |
| Reads | `a.venues.Screen` → `tenant.VenueScreen` (`internal/domain/tenant/venue.go:333-345`); `a.locationsPlaques` → `tenant.PlaqueScreen` (`plaques.go:137`; type `plaque.go:596`, carries `Zone`) |
| Mappers | `venueRowViews` (`locations.go:1010-1072`), `fillPlaques` (`plaques.go:148-195`), `plaqueRowView` (`:206-242`), `plaqueStatusWord` (`:244`), `keyStateOf` (`:302`), `keyAlertOf` (`:331`), `plaqueStamp` (`:350`), `gpsText` (`locations.go:1150`, 6 dp), `shiftText` (`:1160`), `ipTexts` (`:1139`) |
| Cards | `?venue=new|<uuid>` → `VenueForm`; `?department=new|<uuid>` → `DepartmentForm`; `?plaque=<uid>` → `PlaqueCard`; precedence venue > department > plaque (`locations.go:300-323`); unresolved ids answered by `unresolvedCardProblem` (`:361-421`) |
| Templates | `pages/locations.templ` (`AdminLocations` :40-332); `components/venue.templ` (`VenueRow` :26, `DepartmentRow` :136, `VenueForm` :189, `venueRefForm` :367, `DepartmentForm` :409, `Removal` :578); `components/plaque.templ` (`PlaqueRow` :49, `PlaqueCard` :196) |
| View models | `LocationsView`; `VenueRowView`, `DepartmentRowView`, `VenueFormView`, `RemovalView`, `DepartmentFormView`, `VenueRefFormView`, `RemovalReceiptView` (`components/venueview.go`); `PlaqueRowView`, `PlaqueFormView`, `PlaqueEventView`, `PlaqueReceiptView` (`components/plaqueview.go`) |
| Write routes (unchanged) | venue / department save and delete, external ref, `plaqueMountHref` / `plaqueReplaceHref` / `plaqueUnmountHref` (`plaques.go:100-104`) |

### 1.2 Selection without JS

New **read-only** query parameter `?at=<location uuid>` selects the venue in the detail column.

- Default (no `?at` or not in the list) = `screen.Venues[0]` (ordered by name, `db/queries/locations.sql:60`).
- List cards are `<a href="/admin/locations?at=<uuid>" aria-current>`; phone links add `#venue`.
- An `?at` that resolves to nothing is **answered** (reuse `unknown-venue` / `unreadable`), checked
  **after** the venue/department/plaque branches so "never complain about a search that never ran"
  holds (`locations.go:372-378`).
- Extend in the same change: `TestLocationsSection_ReflectsNothingFromTheQueryString`,
  `TestLocationsSection_AnUnresolvedCardIsAnswered`,
  `TestLocationsSection_SaysNothingAboutACardItNeverLookedFor`,
  `TestLocationsSection_EveryControlLeadsSomewhereThatExists`.
- After a save `venueReturn` (`locationactions.go:541`) may add `at=<saved id>` (server-derived; the
  allowed-keys test `TestVenueReturn_CannotCarryAnythingTheClientChose` is updated deliberately).
- `?venue=` / `?department=` open → render the form page (§2) instead of list + detail. `?plaque=<uid>`
  → the plaque card inside the detail of the plaque's venue (`?at` implied by `LocationID`); a stock
  plaque's card sits in the "In the box" section.
- **Phone:** list first, one detail after it (tablet pattern) — the accordion would duplicate strings and
  break `TestLocationsSection_SaysWhenAVenueHasNoProofOfPlace` (count of "No proof of place" == 1,
  `locations_test.go:792`).

DOM: `<div class="flex flex-col gap-6 desk:grid desk:grid-cols-[minmax(0,300px)_minmax(0,1fr)] desk:items-start">`
→ list `<ul class="grid grid-cols-1 gap-2.5 md:grid-cols-2 desk:grid-cols-1 desk:gap-2">` → `<section id="venue" aria-labelledby="venue-h">`.

### 1.3 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| V2 | Kicker "8 venues · 11 plaques on walls" | `len(v.Venues)`, `len(v.Mounted)` | DERIVE | capped lists print a floor ("200+") or keep the cap notice beside it; `LocationsView.Summary` built in the handler |
| V3 | Title "Venues & plaques" | shell | EXISTS | |
| V4 | "Order plaques" | none (`locationsview.go` "THERE IS NO AddPlaqueHref"; user decision 2026-08-08) | **REMOVED (D1)** | the existing sentence "Taptime encodes each plaque and loads it here; ask us for the ones you need" (`locations.templ:271-275`) stays in "In the box" |
| V5 | "+ Add venue" | `v.AddVenueHref` (`?venue=new`, `locationactions.go:395`) | EXISTS | one `<a>`, moved with `order-last md:order-none` (phone full width at the bottom) |
| V6 | Venue card | `VenueRowView` | EXISTS + DERIVE | becomes `<a>` (§1.2) |
| V6b | hours "10:00 – 22:00", overnight "18:00 – 02:00 ↷" | `shiftText` → "10:00–22:00 (next day)" | DERIVE | add `ShiftShort` + `Overnight`; `↷` `aria-hidden` + `sr-only` "ends next day"; missing shift still says **"Lateness not measured"** (pinned, `locations_test.go:798`) |
| V6c | badge "IP ✓" / "IP –" | `Ranges`, `NoProofOfPlace` today | DERIVE — **after M12-03** | IP proof only when `len(StaticIPs) > 0 && !netx.TooWideForProofOfPlace(StaticIPs)` (`internal/domain/tap/decide.go:667-722`); third state "IP too wide" (`.ev-badge--wide`); `sr-only` words |
| V6d | badge "GPS ✓" / "GPS –" | `VenueRowView.GPS != ""` | DERIVE | |
| V6e | "2 on the wall" | group `PlaqueScreen.Plaques` by `LocationID` | DERIVE | count only `OnAWall()` plaques (agrees with V2) |
| V6f | warning "No network set — taps rely on GPS" | `!IPProof && GPS != ""` | DERIVE | channel-correct: add "QR scans here go to The Spike"; the "No proof of place" notice (neither) renders **once**, in the detail |
| V7 | Detail eyebrow "Venue", h2 name | `VenueRowView.Name` | EXISTS | |
| V8 | "Edit venue" | `EditHref` (`?venue=<id>`, `locations.go:1065`) | EXISTS | |
| V9 | Plaque drawing | first `OnAWall()` plaque of the venue | **BUILT (D4, M12-12)** | desktop version at every width (the current boards, after visual QA `c2dd88d`, draw an **ink** tap zone with paper words and rings; "paper tap zone" predated that QA — M12-12b follows the boards); **real QR** `<base>/t?tag=<uid>`; "Hold your phone here" / "Or scan — same thing" / `PLAQUE 91AC`; caption "drawing, not to scale"; no plaque → no drawing + "No plaque is on a wall here yet" |
| V9c | caption "{pos} · NFC + QR" | — | **REMOVED (D1)** | no position column |
| V10a | "Venue network" → ranges | `VenueRowView.Ranges` | EXISTS | one per line; empty → "Not set"; too wide → "too wide to count as proof" |
| V10b | Wi-Fi name | `VenueRowView.WiFi` | KEPT AS CODE | **not evidence** (migration 00010; `venue.go:295-303`): outside the evidence list, labelled "Wi-Fi shown at activation (not checked)" (`venue.templ:94`) |
| V10c | "GPS point" | `VenueRowView.GPS` (6 dp) | KEPT AS CODE | 6 dp (`locations.go:1147-1149`); no float re-rounding; empty → "Not set" |
| V10d | "Accepted within 150 m" | `cfg.GPSRadiusMeters` (`TAPPA_GPS_RADIUS_M`, default 150, 25..1000; `internal/config/config.go:190,405`), passed as `checkins.GPSRadiusM()` (`cmd/tappa/main.go:525`) | DERIVE (injection) | add a radius field on `AdminAuth`, wired in `cmd/tappa`; never a literal "150 m" |
| V10e | "Shift … · Europe/Malta" | `Shift` + `PlaqueScreen.Zone` | DERIVE | missing → "Lateness not measured" |
| V10f | radius schematic | static | DERIVE | label from V10d; shown only when GPS is set |
| V10g | footnote | — | DERIVE (copy) | binding text C3 below (channel-aware, baseline-aware) |
| V11 | "Plaques at this venue" table | per-venue `PlaqueScreen.Plaques` | DERIVE | `LocationsView.Detail.Plaques` |
| V11a | "Position" column | — | **REMOVED (D1)** | the uid is the row header |
| V11b | "Plaque" | `PlaqueRowView.UID` | EXISTS | **full uid renders** (`TestPlaquesSection_ShowsTheFiveColumnsAndNeverAKey`, `plaques_test.go:142-152`); each row keeps its open link `?plaque=<uid>` (`TestPlaquesSection_OpensEveryRowItLISTS`) |
| V11c | "Last seen" | `PlaqueRowView.LastSeen` ("2 Jan 2006 15:04", tenant zone) | KEPT AS CODE | absolute ("9 Aug 2026" pinned, `plaques_test.go:146`); **"Never tapped"** pinned |
| V11d | "Chip reads" `641` | `PlaqueRowView.Counter` (`strconv.FormatInt`, `plaques.go:210`) | KEPT AS CODE | **unpadded** (D1); the board's `000641` was corrected |
| V11e | Status pill | `plaqueStatusWord` → "In service" / "In stock" / "Retired" / "Reported lost" (`plaques.go:244-257`) | KEPT AS CODE + DERIVE | "Active" and "Replace" **REMOVED (D1)**; pill "Needs replacing" (`tally--unencoded`) when `KeyAlert == "wall"` (`plaque.templ:68-77`), else the status word; tones In service `tally--mounted`, In stock `tally--stock`, Retired line, Reported lost `tally--rejected` |
| V11f | "Mark lost" | no writer (`tags.sql:389` comment only) | **REMOVED (D1)** | the row's "Open" link reaches the card (Replace, Take it off the wall…) |
| V12 | In stock ("In the box") | `v.Stock`, `v.MountableStock` | EXISTS — **keep** | the only path to mount; rows link `?plaque=<uid>`; "Nothing in stock" pinned (`plaques_test.go:181`) |
| V13 | Out of service without a venue, departments, external ref, "Departments (N)" | `v.OutOfService`, `v.Departments`, `ExternalRef`, `DepartmentCount/Shown` | EXISTS — **keep** | departments + external ref in the detail (rows link `?department=<id>`); pinned strings "Departments (7)", "only 2 named here", "Add a department", "Add a venue first", "Edit Kitchen"; out-of-service sentence "These are kept on purpose" (`locations.templ:312-317`) above the venue table when it lists retired/lost rows |
| V14 | Cap notices, banners, problem words, receipts | `VenuesCapped`, `PlaquesCapped`, `Done`, `Problem`, `RemovalReceipt`, `PlaqueReceipt` (`locations.templ:42-157`) | EXISTS — **keep** | first in content; `ClaimsARemoval` / `ClaimsAPlaqueAct` unchanged |
| V15 | "No venues yet" | `locations.templ:188-197` | EXISTS — **keep** | |
| T1–T2 | tablet grid, "GPS read only at the tap" | — | DERIVE | §4.2-correct |
| M3 | "Late is measured from here, unless a department has its own." | — | DERIVE | correct per §5 — use it |
| M4 | phone plaque cards | same as V11 | DERIVE | single DOM reflow of the table |

### 1.4 Copy (binding)

| Id | Code | Board | Port |
|---|---|---|---|
| C1 | "Lateness not measured" (`venue.templ:86`) | absent | **keep** (pinned) |
| C2 | "None — taps here fall back to GPS" (`venue.templ:61`) | "Not set" | "Not set" + the V6f warning |
| C3 | — | "A tap on the staff network, or within 150 m, is approved on its own." | **"Under the standard rules, a plaque tap on this network or within {R} m is approved. A QR scan needs the network — GPS alone sends it to The Spike. Neither — the tap is still recorded and goes to The Spike. Location is read only at the moment of the tap."** |
| C4 | "No proof of place" + its sentence (`venue.templ:45-51`) | absent | **keep**, exactly once |
| C5 | — | "No network set — taps rely on GPS" | + "QR scans here go to The Spike" |
| C6 | "In service / In stock / Retired / Reported lost" | "Active / Replace / Lost / Retired" | code words + "Needs replacing" |
| C7–C8 | "Chip reads" / "Last seen" | "Counter" / "Last tap" | either; "Never tapped" stays |
| C9 | — | "Hold your phone here" / "Or scan — same thing" / "PLAQUE 91AC" | skill-exact, verbatim |
| C10 | "These are kept on purpose" … | — | keep |
| C11 | "No plaques have been loaded … ask us …" (`locations.templ:271-275`) | "Order plaques" | keep the sentence, no button |

### 1.5 Breakpoints and recipes

Header row `flex flex-wrap items-end justify-between gap-4`. Container and list per §1.2 (300 + 24 +
540 fits 864). Detail row: phone stacked; `md:grid md:grid-cols-[236px_minmax(0,1fr)] md:gap-[22px]`;
desk `desk:flex desk:flex-wrap desk:gap-4`, figure `desk:basis-[260px]`, card `desk:flex-1 desk:basis-[320px]`.
Plaque table `overflow-x-auto`, `min-w-[620px]` from `md`, single-DOM phone reflow. Venue card,
evidence badges, plaque drawing, plaque table row, status pill: README §5. Detail cards `rounded-[18px]
border border-line bg-paper px-6 py-[22px] flex flex-col gap-4`; dl `grid grid-cols-[auto_1fr] gap-x-4
gap-y-2.5`; footnote `border-t border-dashed border-line pt-3 text-[13px] text-ink/70`. Radius
schematic: static classes `.radius`, `.radius-ring1..3`, `.radius-arm`, `.radius-label`, `.radius-dot`
(rings `rounded-full border-dashed`, dot `size-3 rounded-full bg-ink`), `role="img"` + `aria-label`
"Schematic: the venue point with a {R} metre circle around it".

### 1.6 Not ported (Venues)

"Order plaques" / "Order a plaque →" · "Mark lost" · plaque positions ("Front door", "Old front door",
caption "{pos}") · "Active" and "Replace" pills · the phone accordion · tap-zone colour variants (ink,
green) · Wi-Fi under "Where it is" as evidence · "approved on its own" footnote · 3-dp coordinates ·
literal "150 m".

### 1.7 Tests

Stay green (markup-tolerant updates allowed): `TestLocationsSection_EveryControlLeadsSomewhereThatExists`,
`…OffersNoActionTheServerWouldRefuse`, `TestLocationsSection_LoadsNoScript`,
`TestLocationsSection_SaysWhenAVenueHasNoProofOfPlace` (count 1), `…ACappedListNamesItsOwnCount`,
`…ACappedListStillNamesItsCountAfterARejectedForm` (venue **and** department cap notices on the
re-rendered form page, `locations_test.go:847-871`), `…AnEmptyStateIsGatedOnHavingAsked`,
`TestLocationsSection_AnUnresolvedCardIsAnswered`, `…AVenueSaysHowManyDepartmentsItReallyHas`,
`TestLocationsSection_ReflectsNothingFromTheQueryString`, `…NeverClaimsARemovalItCannotSee`.
Plaques: `TestPlaquesSection_ShowsTheFiveColumnsAndNeverAKey`, `TestPlaquesSection_OpensEveryRowItLISTS`,
`…ReadingFailureIsAnErrorNotAnEmptyBox`, `TestPlaquesCard_OpensOnlyWhenNoOtherCardDid`,
`TestPlaqueViewModels_CannotCarryAKey` (shape walk — the QR rides on a separate view type as a string),
`TestPlaqueWords_AgreeBetweenHandlerAndTemplate`.
New (describe, then name when written): `?at=` default / unknown / unreadable; a too-wide list
(`{0.0.0.0/1, 128.0.0.0/1}`) → "IP –" and the "No proof of place" notice; the radius label equals the
injected radius (200 → "200 m"); the drawn path **equals** the `internal/qrcode` SVG-path helper's
output for `<base>/t?tag=<uid>` (the decode round-trip itself is M12-12's test in
`internal/qrcode/svgpath_test.go`, `package qrcode_test` like the package's reader in `read_test.go` — the in-package `qrcode_test.go` cannot call it). No holdingless golden
covers this page.

---

## 2. Venue form — `?venue=new|<uuid>`, `?department=…`

### 2.1 Wiring

Same route and handler (`venueForms`, `locations.go:461-544`). POST `saveVenue`
(`locationactions.go:109`) / `saveDepartment` (`:191`); a rejected form re-renders with 200
(`renderVenueFormAgain` `:285`, `venueFormFromSubmission` `:413`, keeping the raw text). Validation:
`parseVenueForm` (`locations.go:556`), `parseRanges` (`:697-740`, the whole-list
`netx.TooWideForProofOfPlace` check at `:727-739`), `parseGPS` (`:789`), `parseShift` (`:946`),
`parseSSID` (`:995`); limits `maxStaticRanges = 32`, `maxSSIDBytes = 32`, `maxVenueBody = 16 KiB`.
Templates `VenueForm` (`venue.templ:189-365`), `venueRefForm` (`:367`), `Removal` (`:578`),
`DepartmentForm` (`:409`).

### 2.2 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| F1 | Crumbs "Venues & plaques / KF Valletta" | `locationsHref` + `VenueFormView.Name` | DERIVE | link → `?at=<id>` |
| F2 | Title "Edit KF Valletta" | `VenueFormView.Heading` ("Edit " + name / "Add a venue"; "Edit venue" after a rejected edit, `locationactions.go:417`) | EXISTS | Title slot |
| F3 | "Preview as Restaurant / Production" | — | **REMOVED (D1)** | board device |
| F4 | 01 Name | `name="name"` | EXISTS | section number `.secno` decorative, `aria-hidden` |
| F5 | 02 help | — | DERIVE (copy) | **"Two ways Taptime checks a tap really happened here. For a plaque tap either one is enough; a QR scan needs the network."** |
| F6–F7 | ranges label + textarea | `name="static_ips"`, `ariaInvalid(ErrorField,"static_ips")` | EXISTS | "Address ranges — one per line" (commas, semicolons, spaces also accepted, `parseRanges :698-700`); error `border-2 border-tomato` |
| F8 | "How each line was read" | — | **REMOVED (D1)** | backlog; the /7 · /31 rule is about the whole list, a per-line view would need care |
| F9 | Error box | `parseRanges` message via `fieldNote` (`venue.templ:542`) | EXISTS | the box carries the parser's sentence **verbatim**; the board heading "a venue network is at most a /7" **REMOVED** (incomplete: the bound is the list's total, /7 IPv4 **or** /31 IPv6) — no per-class heading |
| F10 | ranges help | `venue.templ:232-239` | EXISTS | examples from `203.0.113.0/24` (documentation range); keep "goes to The Spike" capitalisation |
| F11 | Latitude / Longitude | `gps_lat` / `gps_lng` | EXISTS | both-or-neither messages pinned |
| F12 | GPS help | `venue.templ:283-287` | DERIVE (copy) | "{R} m" from configuration; "The radius is set by Taptime, not per venue." ("fixed" **REMOVED**); keep "empty is not a position of zero"; "backup check" **REMOVED** |
| F13 | radius schematic | static | DERIVE | V10f component |
| F14 | 03 Shift + "Ends next day" | `shift_start`, `shift_end`, `overnight` | EXISTS | checkbox as a bordered label `inline-flex min-h-11 items-center gap-2.5 rounded-[10px] border border-line px-3.5` |
| F15 | shift help | `venue.templ:330-335` | DERIVE (copy) | **"Lateness is measured against the shift of the venue a person taps at, unless their department has its own."** + keep "empty means lateness is not measured here, which is not the same as a shift starting at midnight" |
| F16 | 24 h shift bar | stored / parsed shift | DERIVE | `<svg viewBox="0 0 1440 14" preserveAspectRatio="none" class="h-3.5 w-full rounded-[7px]" aria-hidden="true">` + rects with **integer minutes** (no float); overnight → two rects; omitted when the posted shift does not parse |
| F17 | 04 Wi-Fi (optional) | `wifi_ssid`; help identical (`venue.templ:351-355`) | EXISTS | "It is not used to check anything" |
| F18 | "Save venue" / Cancel | `Submit`, `CloseHref` | EXISTS | Cancel → `?at=<id>` (or the list for a new venue) |
| F19 | Docket "What a tap here can prove" | — | DERIVE | network line from the saved range count ("2 ranges", "Not set", "too wide"), GPS line from values or "Not set"; the "20 + 50 + 30 = 100" trust line **REMOVED** (a second representation of `trustScore`) |
| F20 | Departments card | `screen.Departments` filtered by `LocationID` | DERIVE | shown for **every** business whenever the venue has departments; rows link `?department=<id>`, empty shift → "Uses the venue's shift" (`venue.templ:157-163`); "Add a department" is a link to `?department=new` (the inline mini-form is **not ported**) |
| F21 | "Plaques on these walls" | per-venue `PlaqueScreen` (pass it into the form builder; `locationsShell` has it, `locationactions.go:402`) | DERIVE | uid, not position; link → `?at=<id>#venue` |
| F22 | External ref | `VenueRefFormView` (`venue.templ:367-407`, owner-only via `CanChange`) | EXISTS | edit only; non-owner sees the value + "Only the business owner can change an external ref." |
| F23 | Remove this venue | `RemovalView` (`venue.templ:578-643`) | KEPT AS CODE | the control is **absent** when it would fail (`TestVenueRemoval_IsOfferedOnlyWhenNothingPointsAtTheVenue`, `TestRemoval_IsOwnerOnly_TheControlIsAbsentForAManager`) — the board's disabled button **REMOVED**; Blocked and Reserved stay two sentences; reasons as today |

### 2.3 Breakpoints and recipes

Desk `desk:grid desk:grid-cols-[minmax(0,1fr)_340px] desk:gap-6 desk:items-start` (500 + 340 + gap fits
864); form card `rounded-[18px] border border-line bg-paper px-7 pt-2 pb-7`, sections `flex gap-4
border-b border-dashed border-line py-[22px]`. Tablet: each section its own `.form-card`, the right
column `md:grid md:grid-cols-2 gap-4` below. Phone stacked; lat/lng and start/end `grid-cols-2`.
`.secno` `inline-flex size-[30px] items-center justify-center rounded-full border-[1.5px] border-ink
font-mono text-xs font-bold`. Fields: README §5 (reuse `.filter-field` / `.filter-input` + a rounded
modifier rather than a third input class). Error box `rounded-xl border border-tomato bg-tomato/5
px-3.5 py-3`, `role="alert"`. Danger section `rounded-[18px] border border-tomato bg-tomato/5 px-[22px]
py-5`.

### 2.4 Not ported (form)

Preview toggle · per-line read-back · width heading "at most a /7" · trust arithmetic line ·
production-only departments gating · inline department mini-form · disabled removal button ·
"backup check" / "the radius is fixed" wording.

### 2.5 Tests

Pinned strings: "needs both a latitude and a longitude", "must be between", "must be a plain number",
"cannot be moved to another venue", "Remove this venue", "cannot be removed while anything still
belongs", "reserved for an owner", "confirm=remove", "Edit Kitchen", "Add a department", "Venue could
not be read", "audit trail", "Venue removed"; `parseRanges` messages
(`TestParseRanges_AcceptsWhatPostgresWouldAndRefusesWhatItWouldNot`). Re-check
`TestVenueFormView_IsRebuiltFromWhatWasPostedNotFromWhatParsed`,
`TestSaveVenue_ARejectedFormKeepsTheManagersTyping`, `…OffersNoActionTheServerWouldRefuse` (department
edit has no venue select), `TestRemoval_*` / `TestVenueRemoval_*`. New: the shift-bar rect `x`/`width`
are integers; the form shows only this venue's plaques and departments.

---

## 3. The Roll — `/admin/reports`

### 3.1 Wiring

| | |
|---|---|
| Route | `GET /admin/reports?week=YYYY-MM-DD`; CSV `GET /admin/reports.csv?week=` (`reportscsv.go:70`) |
| Handler | `reportsSection`, `fillReportsView` (`internal/handler/reports.go:156-303`), `reportRowView` (`:305-338`), `hoursMinutes` (`:357-365`, renders **`8h 02m`**, truncating, integer), `weekHref`, `weekCSVHref`, `parseReportFilter`; CSV `reportsExport` (`reportscsv.go:180`), `reportCSV` (`:723`) |
| Domain | `ledger.Reader.Hours` → `ReportScreen` (`internal/domain/ledger/report.go:351-…`); `accumulate` (`:748-…`); `PersonHours` (`:212-267`); `OpenEntry` (`:285-302`, no `EmployeeID` today); `Totals` (`:304-311`); `WeekOf` (`:482-497`); `ReportScreen.TenantName` |
| Query | `ListWorkedShiftEvents` (`db/queries/transactions.sql:663-…`) — no role, no home location today |
| Templates | `pages/reports.templ`: `AdminReports` :38, `reportWeekBar` :74, `reportTotals` :111, `reportPeople` :167, `reportVenues` :236, `reportOpen` :257, `reportPartners` :431, `reportPartnerDocket` :463 |
| View models | `ReportsView`, `ReportTotalsView`, `ReportRowView`, `ReportVenueView`, `ReportOpenView`, `ReportPartnerView` (`reportsview.go`) |

### 3.2 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| R1 | Kicker "Hours per person · week 42" | ISO week of `Period.FirstDay` | DERIVE | `WeekNumber` |
| R2 | Title "The Roll" | shell | EXISTS | |
| R3 | Week stepper ‹ 12 – 18 Oct › | `PrevHref`, `NextHref`; `WeekLabel` (`reports.go:175-180`) | EXISTS + DERIVE | `<a aria-label>`; `WeekShort`; keep "This week" as a link off the current week and the "Week containing" GET form in a compact row |
| R4 | "CSV for the accountant" | `ExportHref` (`reports.templ:96`) | EXISTS | label change allowed (found by href); **keep** "The CSV holds every person and every open check-in — the lists below are shortened, the file is not." (`:103-106`) |
| R5 | Table head | `DayHeadings` | EXISTS | phone letters from `Format("Mon")` |
| R6 | Row header name | `ReportRowView.Name` | EXISTS | |
| R7 | Sub "Line Cook · Valletta" | `employees.role`, `employees.location_id` | **BUILT (D3, M12-21)** | `Role`, `HomeLocationID`, `HomeVenue` on `PersonHours`; empty role → omit |
| R8 | Day cell worked | `PersonHours.Daily[i]` → `hoursMinutes` | KEPT AS CODE | **`8h 02m`** (D1); `h:mm` **REMOVED** (CSV agreement stays) |
| R9 | other-venue mark | home vs counted interval location | **BUILT (D3, M12-21)** | `DayOther[d]`; holding-peer venues count as other; `sr-only` "at another venue" |
| R10 | manual mark "M" | per-day flag in `accumulate` | **BUILT (D3, M12-21)** | the legend says exactly what is counted (arrivals typed, per `report.go:229-254`, or both endpoints — decide in M12-21 and word the legend to match); literal `M` |
| R11 | open mark | `OpenEntry.EmployeeID` | **BUILT (D3, M12-21)** | a day with hours **and** an open check-in shows the hours in the open style + `sr-only` "and an open check-in"; "open" alone only when there are no hours |
| R12 | none "—" | `Daily[i] == 0` (`reports.go:316-319`) | EXISTS | **`text-ink/70`** |
| R13 | total | `ReportRowView.Worked` | EXISTS | `8h 02m` |
| R14 | "+ N open, not counted" | `PersonHours.Open` (`reports.go:334-336`) | DERIVE | never "zero" (Q18) |
| R15 | legend | static | DERIVE | words beside swatches |
| R16 | receipt header "Kebab Factory" / "Week 42 · 12–18 Oct 2026" + zone | `ReportScreen.TenantName` | DERIVE | add `TenantName` to the view; **keep the zone** visible (sub-line) |
| R17 | "People on the books" | `PeopleTotal` = people with hours or open entries | KEPT AS CODE | relabel **"People with hours"** (as the CSV, `reportscsv.go:835`) |
| R18 | "Worked shifts" | `Totals.Shifts` | EXISTS | |
| R19 | "At another venue" | — | **BUILT (D3, M12-21)** | `Totals.OtherVenue` |
| R20 | "Manual entries" | sum of `ManualArrivals` | DERIVE | label truthfully ("Arrivals typed by a manager") |
| R21 | "Approved from the spike" | endpoints with `Verdict=="flag" && Review=="approved"` (`endpointState`, `report.go:669-682`) | **BUILT (D3, M12-21)** | `Totals.Approved` |
| R22 | "Open check-ins" | `Totals.Open` | EXISTS | |
| R23 | TOTAL | `Totals.Worked` | EXISTS | `8h 02m` |
| R24 | open box + "Close open check-ins →" | `Totals.Open` | DERIVE | heading **"Not closed yet."** ("Incomplete." clashes with the truncation alert, `reports.templ:40-50`, pinned by `TestReportsSection_RefusesToPresentATruncatedReadAsATotal`); link `/admin/anomalies`; keep the Q18 sentence "The figure above is SHORT by whatever these add up to. Nothing here is counted as zero hours …" (`reports_test.go:148`) |
| R25 | footer "— PUNCHLESS · TAPTIME —" | static | DERIVE | |
| R26 | awaiting / refused hours, training taps, began before this week, missed checkout, names no employee, the attribution sentence | `Totals.Awaiting/Refused/TooLong`, `PracticeTaps`, `StartedEarlier`, `Unattributable` (`reports.templ:111-165`) | EXISTS — **keep at every width** | extra receipt lines; the phone receipt shows them too (`TestReportsSection_PrintsEveryFigureItHoldsBackFromTheTotal`) |
| R27 | per-person tallies | `ReportRowView.Late/Unmeasured/Awaiting/Refused/Manual/Open` | EXISTS — **keep** | second line in the row header (`font-mono text-[11px]`) or `.tally` chips (`TestReportsSection_MarksManualEntriesAndUnmeasuredArrivals`) |
| R28 | "Per venue" list | `ReportsView.Venues` | EXISTS — **keep** | compact list under the grid |
| R29 | "Open — no checkout" list | `ReportsView.Open`, `needsActionLine` | EXISTS — **keep** | pinned: `TestReportsSection_TheOpenListDoesNotClaimWhyAnEntryIsOpen`, `…BoundsTheOpenListAndStillCountsAllOfIt`, `…SaysHowMuchOfTheBacklogNeedsAction` |
| R30 | cap notice, truncation alert, empty state, partner section | `PeopleShown/Total`, `Incomplete`, `Queried`, `Partners/PartnerManual/PartnersMore` | EXISTS — **keep** | `templ.Join(reportOpen(v), reportPartners(v))` keeps holdingless bytes |
| T1 / M1 | receipt first; phone person cards | — | DERIVE | phone keeps every receipt line (R26) |

### 3.3 Breakpoints

- Body `flex flex-col gap-6 desk:grid desk:grid-cols-[minmax(0,1fr)_minmax(280px,320px)] desk:gap-7
  desk:items-start`; DOM order receipt first, grid second; desk places the receipt right
  (`desk:col-start-2 desk:row-start-1`, grid `desk:col-start-1 desk:row-start-1`). At 864 px the grid
  column is ~540 px → the table scrolls inside its card (`min-w-[690px]`).
- Receipt inner: phone `flex flex-col gap-2.5`; `md:grid md:grid-cols-[1.1fr_1fr] md:gap-[22px]`;
  `desk:flex desk:flex-col desk:gap-3.5`.
- Grid from `md`: wrapper `overflow-x-auto rounded-[18px] border border-line bg-paper pt-2 pb-1`, table
  `w-full min-w-[690px] desk:min-w-[720px]`.
- Phone: **single DOM** — `tr.roll-row` reflows with `max-md:grid max-md:grid-cols-7 …`, `thead
  max-md:sr-only`, day cells carry a `md:hidden` letter span; no second card list (it would double every
  string).

### 3.4 Recipes

Day cell variants, receipt (`.docket`, sharp, perforated), stepper: README §5. Row header name
`font-display text-[15px] font-bold`, sub `font-mono text-[11px] text-ink/70`; total `font-mono
text-base font-bold`. Open box `rounded-[10px] border border-dashed border-saffron bg-saffron/10 px-3
py-2.5 text-[13px]`, link `inline-flex min-h-11 items-center font-bold`. Legend row `flex flex-wrap
gap-[18px] border-t border-line px-5 py-3 font-mono text-xs text-ink/70`.

### 3.5 Not ported (Roll)

`h:mm` format · "People on the books" · "Incomplete." for open check-ins · phone receipt with TOTAL only
· dropping per-person tallies / per-venue list / open list.

### 3.6 Copy (binding)

| Code | Board | Port |
|---|---|---|
| "Download CSV" (`reports.templ:96`) | "CSV for the accountant" | board OK |
| "Hours worked this week" + `40h 00m` | "TOTAL" | keep the code format |
| "N check-ins with no checkout" | "+ 1 open, not counted" | "+ N open, not counted" |
| "These totals are incomplete" (truncation) | "Incomplete." (open) | keep the truncation heading; the open box is "Not closed yet." |
| `WeekLabel` + "· Europe/Malta" | "12 – 18 Oct" | keep the zone somewhere |
| "A shift counts on the day it STARTED…" | — | keep |

### 3.7 Tests

Goldens `web/templates/pages/testdata/holdingless/reports.golden` (`TestHoldinglessPanels_AreByteIdenticalToTheGolden`)
and `internal/handler/testdata/holdingless/reports-view.golden` (`TestHoldinglessViews_AreByteIdenticalToTheGolden`)
change → dedicated re-render commit with the byte-check, plus an assertion that a render with partner
fields minus the partner section equals the holdingless render (CLAUDE.md §9 byte-identity).
`reports_test.go`: `TestReportsSection_PrintsEveryFigureItHoldsBackFromTheTotal`,
`TestReportsSection_TheOpenListDoesNotClaimWhyAnEntryIsOpen`,
`TestReportsSection_RefusesToPresentATruncatedReadAsATotal`, `…ListsAtMostMaxReportRowsAndSaysSo`,
`…BoundsTheOpenListAndStillCountsAllOfIt`, `…OffersNoWriteAtAll`,
`…EveryControlLeadsSomewhereThatExists` (anomalies link), `TestReportsView_HasNoFieldACoordinateCouldTravelIn`
(new names `HomeVenue`, never `HomeAddr` / `EmployeeCode`), `TestHoursMinutes_TruncatesRatherThanRounds`,
`TestReportsSection_MarksManualEntriesAndUnmeasuredArrivals`, `…SaysHowMuchOfTheBacklogNeedsAction`.
`reports_db_test.go`: `…PairsAnOvernightShiftAcrossMidnight` (the cell lands on the start day),
`…NeverRendersACoordinateOrASourceAddress`. `reportscsv_test.go`: `TestReportCSV_AgreesWithTheScreenItWasExportedFrom`
(unchanged — format kept), `TestReportCSVSource_UsesNoFloatAndNoFloatReturningAccessor`,
`TestReportsExport_IsReachableFromTheScreenAndNamesTheWeekOnIt`. Ledger: new table cases in
`internal/domain/ledger/report_test.go` (other venue, manual, open on a worked day, approved from a flag,
holding peer as other); `ListWorkedShiftEvents` stays `residue` in the evidence inventory.

## 4. Red lines (all three screens)

| Risk | Rule | Mitigation |
|---|---|---|
| "IP ✓" for a list wider than /7 · /31 | §5 read side | M12-03 first; badges use `netx.TooWideForProofOfPlace` |
| "approved on its own" / "either one is enough" | §5 QR row; baseline is tenant-changeable | C3 / F5 wording |
| hard-coded "150 m" | `TAPPA_GPS_RADIUS_M` (25..1000) | inject the radius; one source |
| Wi-Fi shown as evidence | migration 00010 | outside the evidence list, "not checked" |
| real QR | skill: ink on paper, no `?src=qr` | D4; separate view type, string path from the `internal/qrcode` helper; decode round-trip in `internal/qrcode/svgpath_test.go` |
| §4.7 | `TestPlaqueViewModels_CannotCarryAKey` | QR data never a slice on a plaque view type |
| hours | §6 no float | integer `Duration` math |
| §4.6 on the Roll | Q18 | every excluded quantity visible at every width |
| holdingless goldens | CLAUDE.md §9 | dedicated re-render + "holding adds only the partner section" assertion |
| `?at=` | §4.6 answered-when-unknown | server lookup in the read list |
| partner rows in "other venue" | K-39 | durations and a mark only; no trust / IP / GPS on the Roll |
| disabled removal button | "control absent when it would fail" | absent + sentence |
</content>
</invoke>
