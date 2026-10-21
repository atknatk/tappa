# LP-6 public pages, builder B: /security, /for/restaurants, /for/production

> **Copied into the repo 2026-10-21** from the approved builder inventory (independent audit, two rounds +
> the S36 wording fix) as the claim-level source for [README.md](README.md) Part II and the M14-14…M14-24 cards.
> `file:line` refers to HEAD `7521dfe` (re-find by symbol). `lpgen/…`, `qa/…` and `bo/project/` are the
> generator and QA paths outside the repo; the boards themselves are in `docs/design/panel-redesign/boards/`.
> Test names that do not exist yet are written as descriptions ("new test: …"), never as identifiers
> (`TestEveryNamedTestExists`); the builder names them.

Boards (generated, never hand-edited): `bo/project/` `Security`, `T-Security`, `M-Security`, `ForRestaurants`,
`T-ForRestaurants`, `M-ForRestaurants`, `ForProduction`, `T-ForProduction`, `M-ForProduction` (`.dc.html`).
Source: `lpgen/gen_b.py`. It imports the shared chrome from `lpgen/chrome.py` (builder A): header with the LP-6 nav,
the phone `<details>` Menu, the footer Product column, helmet CSS, tokens and icons. It also imports the landing's
pilot strip, CTA, phone drawing and mini panel window (`gen.strip`, `gen.cta`, `gen.phone`, `gen.win`) from
`lpgen/gen.py`. Canvas entries: `lpgen/canvas-B.json`. Heights: `lpgen/heights_b.json` (measured).

| Board | w | h (measured) | title |
|---|---:|---:|---|
| Security | 1440 | 7628 | L05 · Security |
| T-Security | 834 | 9542 | Tablet · Security |
| M-Security | 390 | 13286 | Mobile · Security |
| ForRestaurants | 1440 | 5103 | L06 · For restaurants |
| T-ForRestaurants | 834 | 5948 | Tablet · For restaurants |
| M-ForRestaurants | 390 | 7241 | Mobile · For restaurants |
| ForProduction | 1440 | 4367 | L07 · For production |
| T-ForProduction | 834 | 4584 | Tablet · For production |
| M-ForProduction | 390 | 6134 | Mobile · For production |

Render QA (`qa/run.py` on the nine boards, fix round 2026-10-21, run by builder A): 0 findings (hOv / txt / bp / holes /
err all 0) with the phone Menu closed AND open, and the preview height equals the measured height on every board.
Phone boards take the chrome's `menu` prop (closed | open). The footer now carries the marketingChrome claim paragraph
(chrome.footer), and the phone wordmark is ink (base.templ:174-176), as on every public board.

**Fix round (audit, 2026-10-21) — what changed on these boards:**
- Roll miniature: the product's cell looks and legend words (reports.templ rollLegend / `.roll-cell--*`, input.css
  1439-1445): worked = green/15, "at a venue other than their home venue" = inset 1 px ink ring, "open check-in, not
  counted" = dashed saffron + saffron/10, "—" = "no hours counted". Old legend "Dashed: worked at another venue · open:
  …" is gone (R12, R15 updated).
- The IGNORED docket prints direction NONE (components/docket.templ docketDirection: an ignored tap has a nil Type).
- No plaque position names: "KM PLANT · GATE" became the department label (D1: no position column).
- Shift-change queue uses meat-production people who are not already IN elsewhere on the page: Saviour Bonnici and
  Dragan Nikolic (seed.sql KM Meat Production), Karl Azzopardi (invented); Saviour's repeat is the IGNORED tap.
- KM naming follows the seed everywhere (coordinator decision n3): venue "KM Plant"; departments with their seed names
  ("KM Pastry & Bakery", "KM Meat Production", …) on dockets, lanes and timeline rows.
- Maria Vella's cover tap moved to Thursday 10:04 at KF Msida, the day The Roll marks her as at another venue, so she
  is never at two venues at one time across pages (landing hero: Tue 09:58 at KF St Julians).
- Week-42 Roll rows are identical to the landing/product Roll (Luke Borg 8h 06m Monday overnight, 48h 26m).
- The venue table is a real `<table>` with a `<caption>` ("Sample setup · from our demo data"), not inside role="img".
- GPS figure: the "inside" phone and label moved so the label no longer crosses the circle.
- Security ladder: footnote names the six guardrails not drawn (S36). Production debounce sentence says "(the default
  window)" (P7).

Legend:
- **VERBATIM**: an existing `landingview.go` value or landing furniture, used unchanged.
- **NEW CLAIM**: new copy. In the port it becomes a value with a Fact and a rendered-and-claimed test (LP-1).
  `file:line` refers to the working tree at the time of writing; re-find by the symbol named.
- Sample data: every name, time, uid and network on these boards is either invented (names, times) or copied from
  `test/fixtures/seed.sql` (venue and department names, hours, networks). Each figure says so ("sample", "from our
  demo data").

## Verbatim strings reused (no new Fact)
- `LandingEvidence` (all four cards: Proof / Label / Body), the security H2 "Four proofs on every single tap." (now
  the /security H1), `LandingSecurityLede`, `LandingPrivacyNote` (whole, and its fragments "GPS is read only at the
  moment of the tap." and "Built for GDPR from day one."). The fragment "Anything unverified is stored, flagged, and
  queued for manager review — never silently approved." comes from Evidence[3].Body.
- `LandingAudiences`: Kicker, Title, both Points and the Example chip of both cards. Paragraph() is the hero lede,
  and the points are reused as section subs.
- `LandingComparison` rows "New-hire setup", "When someone leaves", "If it breaks", "Internet down on site" (Aspect +
  Taptime column), and "None collected, ever".
- `LandingPricingIncludes`: "Locations & departments, each on its own hours", "Hours & headcount reports, CSV
  export for your payroll flow", "Manual entries, marked apart from tapped ones".
- `LandingFAQ[1]` (Q + A, "What if the phone has no NFC?").
- `LandingSteps[2]` fragment "Built for wet or floury hands."
- `LegalPages[3].Lede` second sentence: "Taptime sets no advertising or analytics cookie and embeds nothing from
  another site."
- Furniture: "Start free — 3 months on us" (FreeMonths), the CTA block ("Go punchless.", ctaLine, ctaBtn), the pilot
  strip, the hero ticket's "SUN valid ✓".
- Product strings: "Entered by a manager" (components/docket.templ:107), "Open check-ins, not counted"
  (pages/reports.templ:157), "1 late arrival · 0h 02m" format (internal/handler/reports.go:389), the
  cross-location note "tapped at a location other than the employee's home location (recorded as cross-location)"
  (internal/policy/baseline.go:330-332), "TRUST 70 / IP yes · GPS n/a" docket evidence wording (panel boards).

## NEW CLAIMS — /security (S)

| # | Copy | Code fact | Proposed Fact |
|---|---|---|---|
| S1 | "The checks run in the server, not on the phone." | `tap.Decide` is server-side and pure (internal/domain/tap/decide.go:24-50); the phone only posts the tap (internal/domain/checkin/checkin.go) | `FactDecisionIsServerSide` |
| S2 | "Moment, person, place — and a backup for place, for when the network can't answer." (sub) | CLAUDE.md §5 "Dört kanıt"; GPS used when IP does not match (decide.go:715-760) | delegates to `LandingEvidence` Sources |
| S3 | "On every read the chip writes a fresh link: the plaque's id, a counter that only goes up, and a 16-character signature (CMAC) made with the plaque's AES-128 key." + figure "tag= / ctr= / cmac=16 hex" | internal/sun/params.go:12 (URL shape), :34 `cmacHexLen = 16`; internal/sun/cmac.go:73 (AES-128 per-tag K_sdmfileread) | `FactChipLinkCarriesCounterAndCMAC` (delegates `SourceSUNURLCarriesCounterAndSignature`) |
| S4 | "The server checks the signature, then moves the plaque's counter forward in one database statement that refuses to go backwards. A second use of the same link finds the counter already past it and is rejected — and the rejected tap is still recorded." + counter figure rows (accepted / same link again rejected / older link rejected) | db/queries/tags.sql:5-9 `AdvanceTagCounter` strict `<`; internal/domain/checkin/checkin.go:1125 (SUN-invalid = recorded reject), :1499 (every decided tap is written) | `FactCopiedLinkIsRefused` (exists) + `FactRejectedTapIsRecorded` |
| S5 | "Plaque keys are stored encrypted under a separate key and are never written to a log." | internal/sun/keys.go:12-16 (AES-256-GCM under TAPPA_TAG_KEK); CLAUDE.md §4.7 | `FactPlaqueKeysAreWrapped` |
| S6 | "A phone gets its session once: the employee opens their invite link, agrees, and makes a first real NFC tap at one of your plaques. That tap writes no record; the next one clocks in." + figure (01 Opens the invite link · 02 Agrees on this phone · 03 First NFC tap at your plaque → "Session on this phone — no record written") | docs/adr/0026 (Karar); CLAUDE.md §5 "Aktivasyon dokunuşu" | `FactActivationCompletesOnFirstNFCTap` |
| S7 | "The phone keeps a random 256-bit token. The database keeps only a keyed hash of it, so the stored value cannot be used to sign in." + figure boxes | internal/session/token.go:58 (`tokenBytes = 32`), :196 (HMAC-SHA-256) | `FactSessionTokenStoredAsKeyedHash` |
| S8 | "When you switch someone off, their next tap is rejected and the attempt is logged as a security event." + ladder chip "security event" | internal/policy/guardrails.go:440 (`sys:employee-deactivated` EffectDeny + Alert), :139 `AlertDeactivatedEmployeeTapped`; checkin.go:933 | `FactDeactivatedPersonIsRefused` (exists) + `FactDeactivatedTapRaisesAlert` |
| S9 | "The address that counts is the one the request arrives from, checked against the ranges saved for the plaque's venue. The Wi-Fi name is not evidence." | decide.go:715 `ipMatches(in.SourceIP, in.LocationIPs)`; internal/handler/locations.go:49 (wifi_ssid "NOT evidence") | `FactIPIsTheRequestSource`, `FactWifiNameIsNotEvidence` |
| S10 | "A list of ranges only counts if it is narrow enough to describe one place: at most a /7 of IPv4 addresses, or a /31 of IPv6. A wider list is refused when you save it and is not counted when a tap is judged." + width-scale figure ("limit /7", "Narrow enough …", "Too wide …", "IPv6 follows the same rule, with the limit at /31.") | internal/netx/prefix.go:33-36, :175-176; write side internal/domain/tenant/venue.go:337; read side decide.go:722 | `FactTooWideListIsNotEvidence` |
| S11 | "The tap page asks the phone for its position once, when the button is pressed. Nothing reads it in the background, before or after." | web/static/js/tap.js:10, :117 (`getCurrentPosition`, one call, click-started) | `FactPositionReadOnlyOnPress` (exists) |
| S12 | "The radius is a deployment setting, 150 m by default. A position inside it counts as proof of place; a position outside it sends the tap to a manager, even if the network matched." + GPS figure ("radius: 150 m by default", Inside / Outside) | internal/config/config.go:405 (`TAPPA_GPS_RADIUS_M`, 150); internal/policy/baseline.go:403 (`base:gps-conflict-review` EffectReview) | `SourceGPSRadiusDefault` (exists) + `FactGPSOutsideRadiusIsReviewed` |
| S13 | "A QR check-in has no signature, so a position alone is never enough for it: it needs the venue's network." | baseline.go:307 (`base:qr-requires-ip`) | `FactCodelessTapNeedsAddress` (exists) |
| S14 | H2 "Built-in rules first, your rules next, a person last." | guardrails (first match), baseline, internal/policy/evaluate.go:43-45 (no match → review for taps) | `FactDecisionOrder` |
| S15 | "Nothing is approved by default. When no rule decides a tap, it is flagged for a manager." + rung 3 "Nothing decided · Locked · the engine's own last rung · No rule above decided the tap → FLAGGED, queued for a manager" | evaluate.go:43-45 | `FactUndecidedTapIsFlagged` |
| S16 | Ladder rung 1 "Guardrails · Built in · no business can switch them off · first match wins": Plaque not in service → REJECTED; Signature fails, or the counter did not move → REJECTED; No session on this phone → activation page · nothing recorded; Account switched off → REJECTED + security event; Same person again within 60 seconds → IGNORED | guardrails.go:371 (`sys:tag-not-active`), :392 (`sys:sun-invalid`), :409 (`sys:no-session` redirect), :440, :547 (`sys:person-debounce`) | `FactGuardrailRows` (derive from `policy.Guardrails`) |
| S17 | Ladder rung 2 "Your rules · The baseline · you can change these · most specific wins, then the strictest": network or GPS inside → APPROVED; GPS read but outside → FLAGGED; QR without network → FLAGGED; neither → FLAGGED | baseline.go (`base:ip-or-gps-ok`, `base:gps-conflict-review`, `base:qr-requires-ip`, `base:no-evidence-review`); evaluate.go doc (scope, then strictest) | `FactBaselineRows` (derive from `policy.Baseline`) |
| S18 | Trust score docket "Every tap 20 · Venue network + 50 · GPS inside the radius + 30" + "Every record carries this score. It is shown on the record and decides nothing: the rules do." | decide.go:650 `trustScore`; decide.go doc "Trust … INDEPENDENT of the verdict" | `FactTrustScoreIsInformational` |
| S19 | "Debounce is per person" + "A queue of people can tap the same plaque one after another, and every tap is recorded. Only the same person tapping again within 60 seconds is ignored." | guardrails.go:547 (input is the person's own gap); config.go:408 (`TAPPA_DEBOUNCE_SECONDS`, 60) | `FactDebounceIsPerPerson` |
| S20 | "The 60 seconds are timed on the server's clock, so a phone's clock cannot shorten them." | decide.go:560-580 (gap = smaller of declared and database-measured distance; ADR 0006) | `FactDebounceUsesServerClock` |
| S21 | Footnote "Guardrails also refuse a tap page that is too old and a declared time that is implausible. The 60-second window and the 150 m radius are deployment settings; the figures shown are the defaults." | guardrails.go:475 (`sys:tap-freshness`), :528 (`sys:occurred-at-bound`); config.go:405, :408 | delegates |
| S22 | H2 "Flagged, never dropped." + flow figure (1 The tap: "Real NFC tap, no venue network, no GPS" → 2 FLAGGED docket → 3 The Spike "Waiting for review" Approve / Reject → 4 The Roll "Approved — hours counted / Rejected — hours refused / Until then — waiting, not counted") | `FactFlaggedQueueDecidedByAManager`; internal/domain/ledger/report.go:782-795 (`endpointState`) | `FactFlaggedQueueDecidedByAManager` (exists) + `FactAwaitingHoursHeldOut` |
| S23 | "A manager decides each flagged record once, and nobody can decide on their own record." | db/migrations/00005…sql:277 (`UNIQUE (transaction_id)`), guardrails.go:614 (`sys:no-self-review`) + the 00005 insert trigger (KISIT 3) | `FactReviewOnceAndNotOwn` |
| S24 | "Until then its hours are held out of the total, and the Roll says how many are waiting." | report.go:782-795 (HoursAwaiting); pages/reports.templ:158 ("Waiting for a decision") | `FactAwaitingHoursHeldOut` |
| S25 | "The decision is stored beside the record; the record itself never changes." | 00005: `transaction_reviews` is its own append-only table (:304 trigger); `transactions_no_mutation` (:200) | `FactReviewIsASeparateRow` |
| S26 | H2 "Records are never edited. Corrections are new records." | CLAUDE.md §4.3; 00005:194-203 | `FactTransactionsImmutable` |
| S27 | "The database refuses to change or delete a stored record — for the application, and for the database owner's own account too." | 00005:194-195 (REVOKE UPDATE, DELETE from tappa_app), :200 (trigger covers the owner role) | `FactTransactionsImmutable` |
| S28 | "When a shift needs fixing, a manager adds an entry by hand. It is marked "Entered by a manager", stays apart from tapped records in reports, and the action is written to an audit log." + figure (original record "unchanged", the manual OUT, "AUDIT LOG manual entry by a manager WED 09:12" sample) | internal/domain/manual/manual.go:178 (audit_log action); components/docket.templ:107; `FactManualEntryNamesAManager` | `FactManualEntryNamesAManager` (exists) + `FactManualEntryIsAudited` |
| S29 | H2 "No fingerprints. No tracking. No ad cookies." + tile titles "No biometrics", "No background location", "No ad or analytics cookies" | `FactNoAuthenticatorAPI`, `FactPositionReadOnlyOnPress`, `LegalPages[3].Lede` | delegates |
| S30 | "Nothing in the product reads a finger or a face. The phone's own screen lock stays the phone's." | `FactNoAuthenticatorAPI` (landingview.go:312); CLAUDE.md §4.1 | `FactNoAuthenticatorAPI` (exists) |
| S31 | "There is no geofence and no tracking between taps." | CLAUDE.md §4.2; redline R2; tap.js:10 | `FactPositionReadOnlyOnPress` (exists) |
| S32 | H2 "Every business is walled off in the database itself." + "Every record carries the id of the business it belongs to, and the database filters every query by it. The application connects with a role that cannot switch that filter off, and its queries filter by business a second time." + figure ("ROW-LEVEL SECURITY", "Each row carries its business. A query from one side returns none of the other's.") | 00005:178-188 (ENABLE + FORCE RLS, policy on tenant_id); scripts/db-init/01-roles.sql:67 (`tappa_app … NOBYPASSRLS`); CLAUDE.md §4.5 | `FactTenantIsolationByRLS` |
| S33 | "When Taptime's own staff look into an account, they use a separate sign-in, and every look is written to an operator audit log." | docs/adr/0020 (separate operator identity, internal/operatorauth); docs/adr/0021 (`op_*`); db/migrations/00026…sql:325 (`operator_audit_log`) | `FactOperatorAccessIsAudited` |
| S34 | "The one sanctioned crossing is a group Taptime sets up for companies that belong together: their staff can clock in at each other's venues, and the record lands with the person's own employer." | docs/adr/0027; CLAUDE.md §4.5 (2), §5 Holding (record written at the employer; membership only via `op_*`) | `FactHoldingIsTheOnlyCrossing` |
| S35 | H2 "Where your records live." + "Hosted in the EU" + "The production server runs in Falkenstein, Germany, and database backups are encrypted before they are stored, in an EU-jurisdiction bucket." | deploy/README.md:6800-6801, :6817 (node fsn1 = Hetzner Falkenstein, measured); deploy/README.md:835-836, :912 (Cloudflare R2 EU-jurisdiction bucket, rclone `crypt`) | `FactHostedInTheEU` (pinned to deploy/README.md the way `FactPlaquesIncludedAndReplacedFree` is pinned to the handoff). **Needs a user decision; see "Open" below.** |

Hero drawing labels (no claim beyond S1–S13): "One tap · four checks · sample record", "Proof 1–4", "Plaque signature / Phone
session / Venue network / GPS position", "Known person ✓", "IP match ✓", "Backup · not needed", and a sample APPROVED
docket (KF ST JULIANS · Maria Vella · IN 09:58 · TRUST 70 · IP yes · GPS n/a). It matches the landing's hero and
panel samples. Button furniture: "How a tap is decided" (#ladder).

## NEW CLAIMS — /for/restaurants (R)

| # | Copy | Code fact | Proposed Fact |
|---|---|---|---|
| R1 | "Built for a chain where staff cover each other's venues and the bar closes after midnight." | baseline.go:316-332 (cross-location is a note, never penalised); decide.go:533 `resolveDirection` | delegates to R3, R10 |
| R2 | Hero figure: nine venue tiles from seed.sql:135-170 (names + hours, Rusty Bar marked overnight) → "One dashboard · hours per person and per venue" | `FactReportPerPersonAndVenue`; seed | `FactReportPerPersonAndVenue` (exists) |
| R3 | H2 "A shift that ends after midnight is still one shift." + "In or out is decided by the person's last open check-in, so the 02:04 tap closes the 17:58 one." + timeline ("RUSTY BAR 18:00–02:00", IN 17:58 MON, OUT 02:04 TUE, "8h 06m · one shift") | decide.go:533-544; seed.sql:162-163 (Rusty Bar 18:00–02:00 overnight) | `AnchorDirectionFollowsLastOpenEntry` (exists) |
| R4 | "The hours land on the day the shift started, at the venue where it started." | report.go:1002 (`dayIndex(in.At …)`), :1004 ("THE SHIFT'S VENUE IS ITS CHECK-IN'S") | `FactShiftCountsOnItsStartDayAndVenue` |
| R5 | "Lateness is measured against the hours of the venue where the person tapped: Rusty Bar's 18:00, not a daytime venue's 10:00." | CLAUDE.md §5 (department shift, else the TAPPED venue's); decide.go:136-145 (judged on the tapped location's shift); KF has no departments (seed.sql:191-193) | `FactLatenessUsesTheTappedVenuesShift` |
| R6 | H2 "Each venue keeps its own hours and its own network." + table "Sample setup · from our demo data" | `AnchorVenueShiftAndAddress` (migration 00002 shift_start/shift_end + static_ips); seed.sql:135-170 | `AnchorVenueShiftAndAddress` (exists) |
| R7 | "Plaques are bound to a venue, so the venue of a tap comes from the plaque, not from the phone." | db/migrations/00004…sql:29 (tags.location_id NOT NULL); decide.go:113 (`CtxLocationID` = the tag's location) | `AnchorPlaqueBelongsToVenue` (exists) |
| R8 | "A venue's network only counts if the range is narrow enough to describe one place; the venue page refuses a list that is too wide." | internal/domain/tenant/venue.go:337 | `FactTooWideListIsNotEvidence` (S10) |
| R9 | H2 "Staff can cover any venue in the chain." | same tenant plaque → ordinary recorded tap; cross-location is only a note (baseline.go:316-332) | `FactCrossVenueTapJudgedLikeHome` |
| R10 | "A tap at another venue is judged on that venue's evidence and that venue's hours, exactly as a tap at home would be." | baseline.go:316-332 (EffectNote decides nothing, ADR 0029); decide.go:136-145 | `FactCrossVenueTapJudgedLikeHome` |
| R11 | "The record carries a note that the tap was away from home; the employee's screen does not mention it." | baseline.go:330-332 (note text); CLAUDE.md §5 (user decision 2026-10-19: note goes to the record, not the result screen); components/docket.templ:147-152 (docket prints the note) | `FactCrossVenueNoteStaysOffTheEmployeesScreen` |
| R12 | "The Roll counts those shifts as at another venue, so cover is visible without being treated as a problem." + Roll legend "at a venue other than their home venue" (the product's legend words) | report.go:1004-1010 (`OtherVenue`, `Marks.Other`); pages/reports.templ rollLegend | `FactReportCountsOtherVenueShifts` |
| R13 | H2 "Taptime never invents a check-out." | CLAUDE.md §5 "Unutulan çıkış: sistem çıkış kaydı üretmez"; no write path emits `out` without a tap or a manual entry | `FactNoAutomaticCheckout` (tripwire: absence) |
| R14 | "A check-in nobody closed stays open and is listed for a manager, who enters the check-out by hand." + flow ("A check-in nobody closed" → "Listed for a manager" → "Closed by hand · Entered by a manager · OUT 22:05") | report.go:885-906 (`openEntry`); pages/anomalies.templ (Odd shapes); manual entry | `FactOpenCheckInIsListedForAManager` |
| R15 | "Open check-ins are left out of the hours, and the Roll says how many there are." + legend "open check-in, not counted" (product legend words) | pages/reports.templ:157 ("Open check-ins, not counted"); report.go:885-906 | `FactOpenCheckInsNotCounted` |
| R16 | H2 "Hours per person and per venue, ready for payroll." + The Roll miniature (sample, totals computed: 41h 50m, 34h 00m, 48h 26m) + "Sample names and hours, drawn for this page." | `FactReportPerPersonAndVenue`, `FactHoursExportAsCSV` | delegates |

Furniture: eyebrows ("Late nights", "Every venue on its own terms", "Covering another venue", "When someone forgets",
"Day to day", "Hours"), H2 "Hiring, leaving, broken plaques, dead Wi-Fi.", buttons "How a tap is verified" (→
Security) and "How the network check works →" (→ Security#proofs).

## NEW CLAIMS — /for/production (P)

| # | Copy | Code fact | Proposed Fact |
|---|---|---|---|
| P1 | "Built for one site with many teams, each starting at its own hour." | `AnchorDepartmentShift` (00002 departments.shift) | `AnchorDepartmentShift` (exists) |
| P2 | Hero figure "One plaque · five departments · sample setup": KM PLANT gate → KM Pastry & Bakery 04:00, KM Meat Production 05:00, KM Central Kitchen 06:00, KM Warehouse & Supply 08:00, KM General 09:00 (seed names) | seed.sql:194-211 (shifts), :167-170 (KM Plant) | seed |
| P3 | H2 "One gate, five start times." + "Lateness is measured against the person's department shift; someone with no department is measured against the site's own hours." + timeline (five department bars + "No department · site hours 09:00–17:00", marker 04:02) | `AnchorLatenessFollowsDepartment` (ledger resolveShift); CLAUDE.md §5; seed.sql:167-170 | `AnchorLatenessFollowsDepartment` (exists) |
| P4 | Caption "Same minute, different yardsticks: against the bakery's 04:00, Anna's 04:02 is 1 late arrival · 0h 02m on The Roll; against meat production's 05:00, Mark's 04:55 is not late." | `tap.MinutesLate` (decide.go:393); internal/handler/reports.go:389 (label format) | `FactRollCountsLateArrivals` |
| P5 | "The Roll counts each person's late arrivals and the minutes they add up to." | report.go:930-938 (`LateShifts`, `LateBy`); reports.go:389 | `FactRollCountsLateArrivals` |
| P6 | H2 "Every record knows its department." + "A person's department travels on each of their records. The Pass and People filter by it, so each team's day reads on its own." + mini Pass window "Today · filtered by department", filter chip "Department KM Pastry & Bakery" | 00005 (`transactions.department_id`); components/filterbar_templ.go:122 + pages/transactions.templ:364 (The Pass); components/roster.templ:589 (People) | `AnchorDepartmentOnEveryRecord` (exists) + `FactPassAndPeopleFilterByDepartment` |
| P7 | H2 "A queue at the gate, one tap each." + sub "Debounce is per person: people in a line can tap the same plaque one after another, and each tap is recorded. Only the same person tapping again within 60 seconds (the default window) is ignored." + figure (three meat-production people seconds apart APPROVED; the first person again 30 s later IGNORED, direction NONE, "same person, 30 s later") | guardrails.go:547; config.go:408 (`TAPPA_DEBOUNCE_SECONDS`, default 60) | `FactDebounceIsPerPerson` (S19) |
| P8 | "The ignored tap is still written down, stamped IGNORED." | decide.go:44 (ignore → ignored, a recorded verdict); checkin.go:1499 | `FactIgnoredTapIsRecorded` |
| P9 | H2 "Gloves, flour, and no app to install." | `FactTapPageNeedsNoApp`, `FactTapButtonSizedForAWetHand` | delegates |
| P10 | "Every department can share the one plaque at the gate." | a plaque belongs to a venue (00004:29), departments are inside a venue (00002); seed: one KM location | `AnchorPlaqueBelongsToVenue` (exists) |
| P11 | "The tap screen has one button, at least 64 pixels tall." | web/static/css/input.css:232 (`.tap-button` min-h-16); CLAUDE.md §9 | `FactTapButtonSizedForAWetHand` (exists) |

Drawing: the employee's phone is `gen.phone` (pages.Tap drawn exactly, unchanged): "Hello Anna Farrugia", "KM Plant",
one "Tap" button, at 04:02. Names Anna Farrugia, Mark Spiteri, Joseph Galea, Rita Mifsud and Karl Azzopardi are invented; Saviour Bonnici and Dragan Nikolic are seed names (KM Meat Production). "Sample names and times,
drawn for this page."

| S36 | Ladder footnote: "Five of the eleven guardrails are shown. The others refuse a tap page that is too old and a declared time that is implausible, keep a plaque of a business outside your group out of your records, hold a QR scan at a partner business to that venue's own network, keep rule changes to the owner and stop anyone reviewing their own record." | `policy.Guardrails` (guardrails.go:302-623, 11 entries); rulebook.go:874-884 phrases | `FactGuardrailRows` |

**New claim count: 63** (S1–S36, R1–R16, P1–P11). Each row may hold several sentences that rest on the same fact.
11 of the 62 are fully covered by an existing Fact, Anchor or Source.

## Decisions taken where the brief was silent
1. **KM venue name — decided (coordinator n3): "KM Plant" everywhere**, from the seed (seed.sql:167-170). The landing
   hero's rotation record follows: landing.js's Anna Farrugia record changes from "KM Facility" to "KM Plant" and its
   plaque line to "KM PLANT · 2F04-1B9A" (sample data; part of M14-04's rotation edit). Department names follow the seed.
2. **EU hosting is included (S35), flagged.** deploy/README.md documents and measures the node (fsn1, Falkenstein)
   and the EU-jurisdiction encrypted backup bucket. However, the published Privacy page is still a skeleton, and it
   lists "Where the data is hosted and processed, and the processors involved" as a pending fact
   (landingview.go:936). The transactional mail relay is AWS SES eu-central-1, also EU (deploy/k8s/05-config.yaml:175).
   **The user should confirm** before the port. Dropping S35 removes one card; nothing else depends on it.
3. **Nav state.** /security marks "Security" `aria-current`. The industry pages mark nothing: LP-6's header has no
   industry item. They are reached from the footer Product column and the phone Menu.
4. **Defaults footnote.** Debounce (TAPPA_DEBOUNCE_SECONDS) and the GPS radius (TAPPA_GPS_RADIUS_M) are deployment
   settings. The page prints 60 s and 150 m and says once that these are the defaults.
5. **Isolation figure.** It uses "Your business / Another business", not KF / KM, because KF and KM are set up as one
   holding (deploy/README.md:2011). A KF/KM wall would contradict S34.
6. **Shared pieces.** chrome.py (A) has no CTA, strip, phone or mini-window, so those come from gen.py (`gen.cta`,
   `gen.strip`, `gen.phone`, `gen.win`): the approved landing's own section markup.
7. **Lateness is not printed on a docket.** The product's docket carries no lateness. The production page puts it in a
   caption that names The Roll, which is where the product shows it.

## Port feasibility (CSP `style-src 'self'`, no inline style, no new JS)

Every board style is a mock-up convenience; none is copied. The data-driven geometry becomes fixed classes or SVG:

| Figure | Board mechanism | Port | Pinning test (new unless named) |
|---|---|---|---|
| Overnight timeline (r_overnight): Rusty Bar 18:00–02:00 bar, IN 17:58 / OUT 02:04 pins, midnight line, "8h 06m · one shift" bracket | `%` left/right from a 16:00→04:00 axis | **Inline SVG** with presentation attributes (x/width computed in Go from the seed shift and the two sample times, viewBox 0 0 1200 N), the pattern of `operatorpages/qr.go`; labels as SVG `<text>` | new test: the overnight figure matches the shift (bar = Rusty Bar's shift from the venue fixture; "8h 06m" = the ledger's duration format of OUT−IN) |
| Department shift bars + the 04:02 marker (p_shifts) | `%` left/right on a 03:00→18:00 axis | **Inline SVG** (one `<rect>` per department, x/width from `departments.shift_start/end`, a `<line>` at 04:02) or, if HTML is preferred, fixed bucket classes `.shiftbar-h03 … h18` (start) × `.shiftbar-w1 … w15` (length, hours) written literally in the templ | new test: the shift bars match the seed departments; new test: the late caption is tap.MinutesLate (0h 02m = `tap.MinutesLate` of 04:02 vs 04:00, label via reports.go format) |
| /7 width scale (fig_width) | `%` position of the limit on a /32→/0 axis | **Inline SVG**; the limit's x is computed from the same constant `netx` uses (IPv4 /7, IPv6 /31) | new test: the width scale uses the netx limit (reads netx's limit constants, fails if the page prints another prefix) |
| GPS figure (fig_gps) | already inline SVG | unchanged; "radius: 150 m by default" printed from `TAPPA_GPS_RADIUS_M`'s default | new test: the GPS radius is the config default |
| Roll miniature cells | inline styles per cell | the product's own classes (`.roll-cell--worked/--other/--open/--none`) re-scoped under the marketing CSS, or literal copies | new test: the Roll legend words match the product (legend strings equal reports.templ rollLegend) |
| Docket tilts, rail clips | inline transform | `nth-child` classes | — |
| Trust score card (20 / +50 / +30) | text | values from the engine's trust constants | new test: the trust figures match the engine |
| Debounce "60 seconds", radius "150 m" | text + "default" caveat | printed from the config defaults | new test: the printed defaults come from config |
| Guardrail rows + footnote count ("Five of the eleven") | text | count derived from `len(policy.Guardrails(...))` | `FactGuardrailRows` test |

No figure needs JavaScript: there is no animation, toggle or measurement; the phone Menu is the chrome's `<details>`.

## Open / for builder A and the orchestrator
- Resolved in the fix round: the closed-Menu rule now lives in chrome.py's shared CSS (gen_b.py's copy removed); every
  phone board is QA'd with the Menu closed and open.
- canvas.json: the orchestrator merged the nine entries from `lpgen/canvas-B.json` (positions at y=32560, heights as measured).
