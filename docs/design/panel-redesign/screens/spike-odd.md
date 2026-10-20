# The Spike (`/admin/review`) and Odd shapes (`/admin/anomalies`)

Boards: `Spike` · `T-Spike` · `M-Spike` and `Odd` · `T-Odd` · `M-Odd`. Citations refer to
**HEAD `b284899`**. Decisions and constraints: [README](../README.md). Status words as in
[pass-ledger](pass-ledger.md).

## 0. Rules for both screens

| Constraint | Where | Consequence |
|---|---|---|
| No inline `style=` | `adminCSP` (`internal/handler/adminlogin.go:1782`) | bar widths and dots come from the width bucket helper (literal classes in a `.templ` file) |
| No script | `review.templ:21-44`, `anomalies.templ:13-16`; `TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty` | selection = `?focus=` link; paging = link; week = link or GET form; both stay on `@PanelShell` |
| Tailwind scans templ text incl. comments | brand skill | describe utilities in comments, do not write them |
| `@layer components` purged when unused | Tailwind v3 | new classes reach `app.css` only if a `.templ` uses them literally |
| Heading | shell `<h1>` (`admin.templ:428-430`) | kicker and right-hand meta use the M12-01 slots (Kicker, Actions) |

---

# PART S — The Spike

## S.1 Wiring

| Item | Location |
|---|---|
| Route | `GET`/`POST /admin/review` (`adminview.go:292`; `internal/handler/dashboard.go:64`, `:158` in `mountWriting`) |
| Handlers | `reviewSection` (`internal/handler/review.go:138-200`), `reviewDecision` (`:276-390`), `confirmedDecision` (`:224-247`), `queueDocketView` (`:253-257`) → `docketView` (`transactions.go:236-276`), `reviewCursor` (`:490`) |
| Read side | `ledger.Reader.Queue` (`internal/domain/ledger/review.go:158-206`), `queueRecord` (`:220-244`), `Pending` (`:78-102`), `Decision` (`:117-136`); K-39 `nameSiteRecords` (`site.go:68-92`), `partnerNote` (`:135-145`), `PartnerVenueNote` (`:169`) |
| SQL | `ListFlaggedForReview` (`db/queries/reviews.sql:84-137`, **`ORDER BY t.occurred_at DESC`** — newest first, kept), `CountPendingFlagged` (`:55-83`), `GetTransactionReview` (`:139`), `InsertTransactionReview` (`:161`) |
| Template | `web/templates/pages/review.templ` (`AdminReview` :45-140; `queueLine` :146, `doneHeading` :162, `problemHeading` :169, `yoursLine` :188) |
| View model | `pages.ReviewItem{ID, Docket}` (`reviewview.go:15-27`), `ReviewView` (`:29-105`) |

## S.2 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| 1 | Kicker "Every day · newest first · shrinks as you work it" | — | DERIVE | "oldest first" on the board is false; the queue is newest first (D1) |
| 2 | Title "The Spike" | shell | EXISTS | |
| 3 | Meta "N waiting" | `PanelChrome.Pending` (`adminview.go:406-424`) via `queueLine` (`review.templ:146-158`) | EXISTS | keep "not counted" (`!Known`) and "more than N" (`Capped`). The literal "Waiting for review: N" is pinned by `TestReviewBadge_PrintsNothingWhenTheQueueIsMeasuredEmpty` (`review_test.go:137`) and `TestReviewDB_TheQueueCountIsTheQueue` (`review_db_test.go:290`) — move them with any wording change, in the same commit. |
| 4 | Meta "· N decided today" | `transaction_reviews.reviewed_at` (migration 00005:271) | **BUILT (D3, M12-25)** | "decided", not "judged"; since tenant-local midnight; failure → "not counted", never 0 |
| 5 | Zone label | `ReviewView.ZoneLabel` (`reviewview.go:56`) | EXISTS — **keep** | the queue spans days (`reviewview.go:53-56`); mono, in the meta line |
| 6 | Spike motif | — | DERIVE | `hidden desk:flex`, `aria-hidden` |
| 7 | Queue list | `ReviewView.Items` (one page = `QueuePageSize`, `ledger/review.go:59`) | DERIVE | each item `<a href="/admin/review?focus=<id>[&after_at=…&after_id=…]">` with `aria-current`; today every record renders a full docket + form (`review.templ:94-123`) — the port shows one in focus |
| 7a–d | name / when / venue / dir | `DocketView.Who`, `Record.OccurredAt` + zone, `LocationName`, `Direction` | EXISTS / DERIVE | "today 14:26" / "yesterday 02:14" / "Mon 09:02" computed with an **injected** `now` (precedent `manualentry.go:173`) |
| 7e | short "why" | rule sid (`Record.matchedSid`, unexported, `ledger.go:302`) | DERIVE + K-39 | closed map sid → phrase (`no-evidence-review` → "no network, no location", `qr-requires-ip` → "QR without network", `ctr-gap-review` → "counter gap", `gps-conflict-review` → "location outside the venue"; both `base:` and `own:`; fallback "sent for review"). **Partner rows: "at a partner venue", nothing evidence-derived** (`site.go:94-123`). Distance phrases (e.g. "GPS 212 m") **REMOVED (D1)**. |
| 7f | selected state | `?focus=` | DERIVE | `uuid.Parse`; invalid / foreign / decided / not on this page → first item, no error; the param only chooses among loaded records, the POST re-checks everything |
| 8 | Empty state | `EmptyState("Nothing is waiting for review")` gated on `v.Queried` (`review.templ:127-138`) | KEPT AS CODE | keep the gate and the sentence "Decided records stay on The Pass, with the flag they were given and the decision beside it." |
| 9 | Focus docket | `.docket` | EXISTS | perforation both edges (brand) |
| 10 | Label "venue · plaque 0C21" | `DocketView.Venue`, `TagUID` | DERIVE | last 4 of the uid is fine (the plaque prints it) |
| 11 | Name | `Who` | EXISTS | |
| 12 | "IN · 14:26:02 · NFC" | `Direction`, `At` (date-ful), `Record.Channel` (`ledger.go:186`) | DERIVE | add `Channel` to the view (M12-11); keep the date in the time |
| 13 | Stamp FLAGGED | `verdictStamp` (`docket.templ:187-198`) | EXISTS | export as `VerdictStamp`; rotation from the class |
| 14 | Tile "Physical tap" | `Record.Channel`; a flagged NFC row had a valid SUN (§5 row 2) | DERIVE | NFC: "yes" · "chip signature valid"; QR: "QR scan" · "no chip signature on a QR scan"; counter gap: "yes" · "counter gap" from the sid alone. The "+37" size **REMOVED (D1)** (not in D3). |
| 15 | Tile "Who" | `EmployeeName`; a flag row had a session (§5 row 3) | DERIVE | "signed in on this phone" — pronouns **REMOVED (D1)** |
| 16 | Tile "Network" | `Record.IPMatch` → yes / no / n/a | EXISTS | "address in the venue's range" / "address not in the venue's range" / "not checked". Carrier and Wi-Fi SSID texts **REMOVED (D1)** (unknowable; Wi-Fi is not evidence). Partner rows: one merged tile "Network · Location: not shown at a partner venue". |
| 17 | Tile "Location" | `Record.GPSMatch` | EXISTS | "placed the tap at the venue" / "did not place the tap at the venue" / "not checked". **false is ambiguous** (no reading vs outside), so the wording must not choose; "not shared", "declined" and metres **REMOVED (D1)**. Radius from configuration if printed (`TAPPA_GPS_RADIUS_M`). |
| 18 | Tile tone | three-state sign | DERIVE | yes → ok, no → bad, n/a → none; the word carries the meaning, marks `aria-hidden` |
| 19–20 | "Why it is here" + reason | `Record.Note` → `DocketView.Note`; `NoteIsTenants` label (`docket.templ:157-165`) | EXISTS | **the engine's sentence binds**; board prose **REMOVED (D1)**; partner rows keep `PartnerVenueNote` |
| 21 | "rule base:…" | `matched_sid` (selected, `reviews.sql:121`), unexported on purpose (`ledger.go:292-302`) | DERIVE + **K-39** | export `RuleSID`, **blanked in `nameSiteRecords` for partner rows** (ADR 0027 §13 item 5(e)); rewrite the `ledger.go:292-302` comment; security auditor |
| 22 | "trust N" | `DocketView.Trust` | EXISTS | partner rows omit (K-39) |
| 23 | Note input | `<input name="note" maxlength="500" autocomplete="off">` (`review.templ:101-111`; `maxReviewNote` `review.go:131`) | EXISTS | label "Note for the record (optional)"; placeholder neutral "e.g. I saw them on the floor"; unique `id` + `label for`; one `<form>` wraps docket input and buttons (and the phone's sticky bar) |
| 24 | Approve | `name="outcome" value="approved"` (`review.templ:117`) | EXISTS | label **"Approve"** — "— they were there" **REMOVED (D1)** (it asserts presence). Two submit buttons, no default outcome, hidden `id` = the focused record. |
| 25 | Reject | `value="rejected"` (`:118`) | EXISTS | ink word, tomato frame |
| 26 | Keyboard hints A / R | — | **REMOVED (D1)** | `accesskey` would turn a chord into a pay decision |
| 27 | Footer line | post-decision Notice (`review.templ:47-63`) | DERIVE | "Your decision is a new line beside the record. The original tap is never edited." ("verdict" is the engine's word, `ledger.go:194-204`). Keep the Notice ("Recorded as approved", "unchanged"). |
| 28 | Done / problem notices | `Done` / `Problem` / `Was` / `Clipped` (`reviewview.go:73-104`) | EXISTS — **keep** | above the two columns |
| 29 | "Show more" | `MoreHref` (`reviewview.go:67`) | EXISTS — **keep** | end of the queue list |
| T1–T3 | Tablet scroller, 2×2 tiles, 2-col buttons | as above | DERIVE | |
| M2 | Phone pager "‹ 1 of 4 ›" + dots | position in the page; prev/next ids | DERIVE | `<a>` (disabled = `aria-disabled` span); "of N" = `len(Items)` for this page; dots `aria-hidden` |
| M3 | Phone: list hidden | — | DERIVE | `hidden md:block` list, `md:hidden` pager |
| M4 | Phone docket fixed height 640 | — | **REMOVED** | size to content |
| M5 | Phone sticky action bar | — | DERIVE | `.spike-actions` fixed above the bar (`bottom: calc(64px + max(0.5rem, env(safe-area-inset-bottom)))`, `md:static`) + page spacer `pb-[84px] md:pb-0` |

## S.3 Breakpoints

- Two columns: `flex flex-col gap-[22px] desk:flex-row desk:items-start desk:gap-7`; queue section
  `hidden md:block md:-mx-8 md:overflow-x-auto md:px-8 desk:mx-0 desk:basis-[280px] desk:overflow-visible desk:px-0 min-w-0`;
  list `flex gap-2.5 md:min-w-max desk:min-w-0 desk:flex-col desk:gap-2`; focus
  `flex min-w-0 flex-col gap-[18px] desk:basis-[520px] desk:grow-[999]` (280 + 28 + 520 fits 864).
- Docket padding `px-[18px] pt-[22px] pb-6 md:p-6 desk:px-[30px] desk:pt-7 desk:pb-[30px]`; name
  `text-[28px] md:text-[30px] desk:text-[34px]`.
- Tiles `grid grid-cols-2 gap-2 md:gap-2.5 desk:grid-cols-[repeat(auto-fit,minmax(150px,1fr))]`.
  **As built (M12-49 tour, DÜŞÜK-7):** `grid grid-cols-2 gap-2 md:gap-2.5 min-[1340px]:grid-cols-4`;
  a partner record's merged tile is `col-span-2`. Reason: the auto-fit grid drew 3 + 1 at a 1200 px
  window (LOCATION alone on row two); fixed counts never do. 1340 = 248 sidebar + 88 padding +
  308 queue and gap + 60 docket padding + 4 × 150 tiles + 3 × 10 gaps (1334, rounded up) —
  recompute if any of those widths changes ([README §5.1](../README.md)).
- Buttons: phone in the sticky bar (Approve `flex-[2] h-14 rounded-[14px]`, Reject `flex-1`);
  tablet `md:grid md:grid-cols-2 md:gap-3`; desk `desk:flex desk:flex-wrap`, each `basis-[200px] grow`.
- Footer: short form on phone, long from `md` (two spans).

## S.4 Recipes

`components.SpikeDocket(SpikeFocusView)` lives in `components` (so it can call the stamp) and is
built from the **same `DocketView` fields** through one mapper (`reviewview.go:18-22` argues for
one source of the evidence line). Evidence tile, queue item, `.btn--approve` / `.btn--reject`:
README §5. Pager button `a.inline-flex size-11 items-center justify-center rounded-xl border
border-ink/50 bg-paper`; dots `size-2 rounded-full bg-ink | bg-line`.

## S.5 Not ported (Spike)

"oldest first" · "judged" wording · keyboard hints and `.kbd` chips · "— they were there" ·
gendered placeholder and "her own browser session" · carrier / SSID sub-texts · "not shared",
"GPS declined", "40 m · within 150 m", "212 m" · counter "+37" size · per-rule explanatory prose ·
fixed 640 px phone docket · "Your verdict is a new line".

## S.6 Binding copy (Spike)

| Place | Code (binding) | Port |
|---|---|---|
| Count line | "Waiting for review: N · Europe/Malta" (`review.templ:90-93`) + honesty states | board wording allowed only if both states survive and the two pinned tests move with it; keep the zone |
| Empty | "Nothing is waiting for review" + "Decided records stay on The Pass…" (`:128-137`) | keep |
| Note label | "Note (optional)" (`:102`) | "Note for the record (optional)" |
| Approve / Reject | "Approve" / "Reject" | same |
| Post-decision | "The tap record itself is unchanged — it still shows the flag the engine gave it. Your decision is recorded beside it and appears on The Pass." (`:49-52`) | keep; §4.3 vocabulary (`review.templ:7-13`): never "change / correct / fix / delete" |
| Stamp | "FLAGGED" | keep |
| Why | engine sentence + "Your own policy rule"; partner `PartnerVenueNote` | keep |

## S.7 Tests

| Test | File | Effect |
|---|---|---|
| `TestReviewForm_PostsToTheRouteThatIsActuallyMounted` | `internal/handler/review_test.go:916-985` | expects 2 forms for 2 records → update to **1** decision form (action = `PanelSections` href, hidden `id` = focused record) **and** assert every queue item is a GET link with `focus=`; keep the "really mounted" half |
| `TestReviewBadge_PrintsNothingWhenTheQueueIsMeasuredEmpty`, `TestReviewDB_TheQueueCountIsTheQueue` | `review_test.go:130-141`, `review_db_test.go:273-300` | move with any copy change |
| `TestReviewBadge_CappedCountsSayTheyAreAFloor` | `review_test.go:144` | "100+" and "more than 100" survive |
| `TestReviewSection_AFailedReadIsAnErrorAndNeverAnEmptyQueue`, `TestReviewSection_AnUnqueriedViewClaimsNothing` | `review_test.go:62-99` | add any new empty-state words to the forbidden lists |
| `TestReviewDecision_ConfirmsAndSaysTheRecordIsUnchanged` | `review_test.go:337` | "Recorded as approved" and "unchanged" |
| `TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered`, `TestReviewDB_ALongNoteIsCutAndTheManagerIsTold` | `review_db_test.go:637,728` | `maxlength="500"` and the clip banner |
| `TestQueueRecord_CarriesEverythingTheDayRecordDoes` | `internal/domain/ledger/mapper_test.go:115` | unchanged (no new queue column in this port) |
| `TestEvidenceReaders_EveryOneIsClassified` | `internal/db/evidencereaders_test.go:118` | `ListFlaggedForReview` stays `go-blank`; the decided-today count reads `transaction_reviews` only |
| `TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason` | `internal/handler/holdingpanel_db_test.go:33` | **add** a queue case: a flagged partner row shows "not shown at a partner venue", the `PartnerVenueNote`, no sid, no trust, no IP/GPS words |

## S.8 Red lines (Spike)

§4.3 — a decision stays a row in `transaction_reviews` + `audit_log` (`review.go:21-28`); no edit
wording · §4.6 — focus fallback never 404s or blanks; paging and refusal notices stay; counts honest ·
K-39 — every new per-record surface (rule sid, short why) blanked **in `nameSiteRecords`** · §4.7 —
no distance, no `policy_context` · self-review guardrail unchanged · one form, two buttons, no `form=`
attribute · no bulk action (`review.go:49-57`).

---

# PART O — Odd shapes

## O.1 Wiring

| Item | Location |
|---|---|
| Route | `GET /admin/anomalies` (`adminview.go:316`; `dashboard.go:72`) |
| Handler | `anomaliesSection` (`internal/handler/anomalies.go:170-…`), `fillAnomaliesView` and its helpers (`outOfLine`, `basisLine`, `togetherLine`, `closestLine`, `largestGapLine`, `partnerLine`, `policyBucketName`, `parseAnomalyFilter` …) |
| Read side | `ledger.Reader.Anomalies` (`internal/domain/ledger/anomaly.go:382-679`); `AnomalyRowCap=50` (`:99`), `TogetherWithin=20s` (`:113`), `TogetherMinDays=2` (`:122`); partner bucket appended last (`:666-671`) |
| SQL | `db/queries/anomalies.sql`: `CountAnomalySignals` :69, `ListAnomalyPeople` :154, `ListAnomalyVenues` :216, `ListAnomalyPlaques` :262, `ListAnomalyPolicySids` :311, `ListTapsTakenTogether` :344, `ListOpenCheckIns` :476, `CountMaskedOpenCheckIns` :544 |
| Template | `web/templates/pages/anomalies.templ` (`AdminAnomalies` :33-51, `anomalyWeekBar` :55, `anomalyTotals` :75, `anomalyMissingSources` :139, `anomalyPeople` :168, `anomalyVenues` :220, `anomalyPlaques` :261, `anomalyPairs` :298, `anomalyOpen` :331, `anomalyPolicies` :408, `moreRowsLine` :439, `anomalyLostPlaques` :451) |
| View model | `web/templates/pages/anomaliesview.go` — **"IT IS A READ-ONLY VIEW AND THE ABSENCE OF EVERY WRITE FIELD IS THE SPEC … no per-row action href"** (`:5-11`); no severity, no score, every count beside its denominator (`:28-32`) |

## O.2 Elements

| # | Element | Source | Status | Port note |
|---|---|---|---|---|
| 1 | Kicker "Week 42 · 12–18 Oct · nothing here decides anything" | `WeekLabel`, `ZoneLabel`; ISO week of `p.FirstDay` | DERIVE | true (§4.3 read-only); **keep the zone** (`anomalies.templ:36-39`) |
| 2 | Week segmented ‹ 12 – 18 Oct › | `PrevHref`, `NextHref`, `ThisWeekHref` (`anomaliesview.go:59-61`); GET form with a date input (`anomalies.templ:55-68`) | EXISTS (as links) | arrows are `<a>` with `aria-label`; **keep a GET `<form>`** (date + "This week") in a compact row — `TestAnomaliesSection_OffersNoWriteAtAll` uses `method="get"` as its positive control |
| 3 | "Evidence mix by venue" stacked bars | `AnomalyVenue{Records, Judged, Answerable, DistanceOnly, OutsideVenue, Partner}` | **BUILT (D3, M12-22)** | `Both`, `NetworkOnly`, `Neither` added beside `DistanceOnly`; each filter carries `AND t.site_tenant_id IS NULL`; base = `Judged`; integer percents, largest remainder; `role="img"` + `aria-label` with the four counts |
| 3a–b | venue name; count | `AnomalyVenueView.Name`, `Records` | EXISTS | add `RecordsN int`; keep the prose for screen readers |
| 3d | Partner venue row | "Partner venue · evidence not shown" (`anomalies.templ:240-243`) | EXISTS — **keep** | **no bar** for a partner row |
| 3e–f | legend; "share of records judged on evidence" | — | DERIVE | name the base honestly: the bar is out of `Judged`, the right-hand count is `Records` (say so) |
| 4 | "Tapped together" card | `AnomalyPairView`, `TogetherLine` | EXISTS | **20 s, same venue, 2+ separate days** (D1). `TogetherLine` is pinned ("seconds of each other", "separate days" — `TestAnomaliesSection_NeverLabelsAPerson`, `anomalies_test.go:246-249`). Framing "Could be a shared lift. Worth a look, not a conclusion." allowed. |
| 4a | Pair names A •—• B | `First`, `Second` | EXISTS | connector `aria-hidden` + `sr-only` "and" |
| 4b | venue · gap | `Venue`; `Closest` ("closest N seconds apart"); `Together` ("12 times together") | EXISTS | median **REMOVED (D1)**; keep `Together` (the board drops it) |
| 4c | Week dots (7) | `AnomalyPair.Days` (count only) | **BUILT (D3, M12-23)** | `together_days` mapped against `Period.DayLabels`; `aria-label` "Together on Mon, Tue, Thu" |
| 4d | "5 of 7 days" | `Days` | DERIVE | |
| 5 | "Plaque counter gaps" | `anomalyPlaques` copy (`anomalies.templ:263-268`) | KEPT AS CODE | keep "this list does not tell them apart" |
| 5a | "KF Hamrun · back door  PLAQUE 7E55" | `LocationName`, `TagUID` | EXISTS | "back door" **REMOVED (D1)** (no column) |
| 5b | Chip "+37" | `LargestGap` ("37 reads missing"; `ctr_gap = new − old − 1`, `db/queries/tags.sql:45`) | EXISTS | wording "N reads missing" |
| 5c | Tick strip with jump position | — | **REMOVED** | positions would be invented |
| 5d | "#from → #to" + "between 02:14 and 09:30 Tue" | — | **BUILT (D3, M12-24)** | counters **unpadded** (`#604 → #641`), the same `strconv` form the docket uses (no `padStart`); from = to − gap − 1; the previous tap on the same `tag_uid` may precede the window; reads `ctr` → new `evidenceReaders` entry (`excluded`, must `tag_uid IS NOT NULL`, K-30); times are declared `occurred_at` — say "between", with that caveat |
| 5e | Count / denominator chips | `CounterGaps`, `Records` | EXISTS — **keep** | "plaques clean" (tablet) **REMOVED** (not computed) |
| 6 | "GPS-only" per person | `AnomalyPerson.DistanceOnly` | DERIVE | filter `People` with `DistanceOnly > 0`, sort desc; **`People` is capped at 50 and ordered by counter gaps first** (`anomalies.sql:202-214`), so when `MorePeople` the list says it may be incomplete (`moreRowsLine`). Heading "verified by GPS alone, no network match" — "approved" is not measured. Venue line **REMOVED** (people rows carry no venue). Shown on the phone too. |
| 7 | "Left open — not counted in hours" | `AnomalyOpenView{Who, Venue, At, ManagerTyped, WasMaskable}` | EXISTS | keep the definition paragraph, the training-tap paragraph (`anomalies.templ:338-346`), the "Entered by a manager" / "Training tap follows" tallies, the masked notice (`:378-399`, ADR 0008), the shortened notice and the sentence that The Roll counts slightly differently |
| 7a | Chip "open 3d" | `AnomalyOpen.At` | DERIVE | `fillAnomaliesView` gets an injected `now` (the golden test calls it directly, `holdingless_view_golden_test.go:70-71`); integer days |
| 7b | "Close it" | — | **REMOVED (D1)** | the view is read-only by spec |
| 8 | "Which rule decided" | `AnomalyPolicyView{SID, Layer, Records}`; partner bucket "At a partner business's venue (rules not shown)" | EXISTS + DERIVE | add `RecordsN`; max = first row (SQL orders by count, `anomalies.sql:341`) **excluding** the partner bucket; **one neutral tone** (ink fill; partner bucket `bg-line`) — no colour guessed from a sid; keep `Layer` and "No rule recorded". "first match wins" **REMOVED (D1)** (false below the guardrails). |
| 9 | Totals docket | `anomalyTotals` (`:75-125`) | EXISTS — **keep** | first card (or compact summary row); every figure names its base |
| 10 | "What this page does not see" | `anomalyMissingSources` (`:139-160`) | EXISTS — **keep** | `TestAnomaliesSection_SaysWhatItCannotSee` (`anomalies_test.go:178-211`) |
| 11 | Per-person list | `anomalyPeople` (`:168-213`) | EXISTS — **keep** | all four counters |
| 12 | Lost-plaque alert (holding) | `anomalyLostPlaques` joined to totals (`:41`, `:451-480`) | EXISTS — **keep** | `templ.Join` keeps holdingless bytes |
| 13 | "This list is shortened" | `More*` + `moreRowsLine` | EXISTS — **keep** | `TestAnomaliesSection_ShortenedListsSayTheCapAndSayTheTotalsDoNot` (`:430`) |
| 14 | Empty states | `anomalyPeople` / `anomalyOpen` | EXISTS — **keep** | never "all clear" |

## O.3 Breakpoints

> **As built (M12-29, recorded in M12-48):** the desk width uses the **tablet arrangement**, one
> DOM: the evidence mix across the top, the rule breakdown across the bottom, and two columns
> between them (pairs and people left; counter gaps, GPS-alone and open entries right). The desk
> board puts the mix and the rules inside its left column instead. Its own grid does not hold at
> 1200 px, where the content box is 864 px (README §3.6), and one markup cannot carry both column
> assignments without a row-aligned grid, which leaves a gap as tall as the longer list beside it
> (`anomalies.templ`, `AdminAnomalies` comment). Row-level recipes below keep their `desk:` steps.

- Grid `grid grid-cols-1 items-start gap-4 md:grid-cols-2`; mix and rules `md:col-span-2 desk:col-span-1`
  (**as built:** full width at the desk too — the recorded deviation above; the M12-49 tour's
  DÜŞÜK-6 is this deviation, not a defect);
  totals, notices, people and open lists above or as `md:col-span-2` cards. (At 864 px two columns of
  ~424 px.)
- Card `p-5 desk:px-6 desk:py-[22px]`; h2 `text-[19px] md:text-xl`.
- Mix row: phone `flex flex-col gap-1` (name + count, bar `h-3.5`); `md:grid md:grid-cols-[110px_1fr_44px] md:items-center md:gap-2.5 desk:grid-cols-[96px_1fr_44px]`, bar `md:h-[18px]`.
- Legend phone `grid grid-cols-2 gap-x-3 gap-y-1.5 text-[11px]`; `md:flex md:flex-wrap md:gap-3.5 md:text-xs`.
- Pair row stacked on phone; `desk:flex desk:flex-wrap desk:items-center`, count `w-[76px] text-right`.
- GPS-only row `grid grid-cols-[1fr_80px_28px] gap-2.5 desk:grid-cols-[1fr_120px_28px]`.
- Rule row stacked; tablet `md:grid md:grid-cols-[200px_1fr_40px] md:items-center md:gap-3`.

## O.4 Recipes

Card `.odd-card` (`flex min-w-0 flex-col gap-3.5 rounded-[18px] border border-line bg-paper p-5`) —
aggregates may be cards; record-like rows stay dockets. Row separator `border-t border-dashed
border-line pt-3`. Width buckets: README §5. Stacked track `flex h-[18px] overflow-hidden rounded
bg-porcelain`; segments: both `bg-tappa-green`, network only `bg-tappa-green/50`, distance only
`bg-saffron`, neither **solid `bg-line`** (no hatch — "Gradient yok"); separation `border-r
border-paper` on non-last segments (no gap, it would overflow percentages); legend swatch for
"neither" adds `ring-1 ring-inset ring-ink/50`. Week dots `size-3 rounded-full` on `bg-ink`, off
`border-[1.5px] border-ink/50`. Gap chip `font-mono text-xs border border-saffron bg-saffron/10
px-2`. Counter line (with M12-24): `#from` / track with a `bg-saffron-lite` badge / `#to`. Open-age
chip `border-dashed border-saffron bg-saffron/10`.

## O.5 Not ported (Odd)

"Close it" per row · "Within 10 s … same plaque" · median gap · tick strip positions · "back door"
· "plaques clean" · "approved, but off the staff network" · "read 37 times without opening
Taptime" · "first match wins" · hatch pattern · sid-guessed bar colours · venue line under GPS-only
people.

## O.6 Binding copy (Odd)

| Place | Code (binding) | Port |
|---|---|---|
| Week | "Mon 7 – Sun 13 September 2026 · Europe/Malta" | either format; **keep the zone** (golden-pinned → re-render) |
| Pairs | heading "Tapped together, more than once" + `TogetherLine` (20 s, venue, ≥ 2 days) | heading may shorten if `TogetherLine` stays |
| Gap | "closest N seconds apart" | keep |
| Counter gaps | "A plaque's chip counts every read. A gap means reads happened that never became a check-in — … this list does not tell them apart." | keep |
| GPS-only | "N verified by GPS alone" | keep |
| Open | "Check-ins with no checkout" + definition + training paragraph | heading may read "Left open — not counted in hours"; keep both paragraphs |
| Rules | "Which rule decided" + its paragraph; partner bucket label verbatim | keep |
| Partner venue | "Partner venue · evidence not shown" | keep (K-39) |
| Evidence mix | parts "both" · "network only" · "distance only" · "neither", and "not split" for `Unsplit()` | every part's count printed under the bar ("52 both · 9 network only · 3 distance only · 1 neither · 2 not split"); the base beside the bar ("65 judged"); a venue with nothing judged: "No record here was judged on evidence this week." (M12-29) |
| Counters (`JumpState`) | `read`: `#from → #to` (sr-only "Counter #604 → #641, 36 reads missing") + "between Mon 12 Oct 02:14 and Tue 13 Oct 09:30, by the phones' clocks"; `read` with `JumpPreviousAt == nil`: "read Tue 13 Oct 09:30 · the earlier read is not on record"; `failed`: "The counters around this gap could not be read."; `elsewhere`: "The read that showed this gap is not on your record, so no counters are printed."; `""` (not asked): nothing beyond "N reads missing" | written in M12-29; the times are the declared `occurred_at` |
| GPS-only subset | "Taken from the per-person list, which is shortened, so this list may be incomplete." (only when `MorePeople`) | M12-29 |
| sid literals | none | **never write a literal `base:` or `sys:` in `anomalies.templ`** (`TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason`, `holdingpanel_db_test.go:108-114`) |

## O.7 Tests

| Test | File | Effect |
|---|---|---|
| `TestHoldinglessPanels_AreByteIdenticalToTheGolden` | `web/templates/pages/holdingless_golden_test.go:64` (`testdata/holdingless/anomalies.golden`) | re-render in a dedicated commit: old vs new holdingless render shows no partner markup; header paragraph like the 2026-10-20 one; `templ.Join(anomalyTotals, anomalyLostPlaques)` keeps 0 bytes for no lost plaques; new fields zero-safe |
| `TestHoldinglessViews_AreByteIdenticalToTheGolden` | `internal/handler/holdingless_view_golden_test.go:78` | same protocol; the `now` parameter changes the call at `:70` |
| `TestEvidenceReaders_EveryOneIsClassified` | `internal/db/evidencereaders_test.go:47-48` | `ListAnomalyVenues` pinned at 3 occurrences of `site_tenant_id IS NULL` → 6; new from/to statement filed |
| `TestAnomalies_APartnerVenueRowAddsNoEvidenceFigure`, `…_EveryEvidenceFigureLeavesThePartnerOut`, `TestAnomalies_ThePartnerBucketSaysNothingByItsPlace`, `…_ThePolicyListIsCutInSQLAndItsOrderIsTotal` | `internal/domain/ledger/holding_db_test.go:853-1065` | extend "every evidence figure" to the three new mix figures |
| `TestAnomaliesSection_OffersNoWriteAtAll` | `internal/handler/anomalies_test.go:121-170` | POST forms = the two sign-outs; ≥ 1 GET form |
| `TestAnomaliesSection_SaysWhatItCannotSee`, `TestAnomaliesSection_NeverLabelsAPerson` | `:178-211`, `:213-254` | keep the notice and `TogetherLine`; no forbidden words |
| `TestAnomaliesView_CarriesNothingACoordinateCouldHideIn` | `:317-366` (`bannedFieldName`, `internal/handler/reports_test.go:488-506`) | names `Both`, `NetworkOnly`, `DistanceOnly`, `Neither`, `DistanceOnlyPeople` — never `IPOnly`, `GPSOnly` |
| `TestAnomaliesPages_CarryNoDecimalDegreeInEITHEROfTheOtherTwOShapes`, `TestPanelAnomaliesDB_NoCoordinateAndNoAddressReachesThePage` | `anomalies_test.go:379`, `anomalies_db_test.go:416` | integer percents only |
| `TestAnomaliesSection_ShortenedListsSayTheCapAndSayTheTotalsDoNot` | `:430` | add the GPS-only subset notice |

## O.8 Red lines (Odd)

K-39 — partner rows out of every evidence figure (each new FILTER carries `site_tenant_id IS
NULL`); the partner venue row gets the label, never a bar; the partner bucket stays anonymous and last
· §4.3 — read-only by spec, nothing posts · §4.6 — every honesty item stays; a capped subset says so
· §4.7 — counts, booleans, `ctr`, timestamps only · §6 — integer percents · holdingless byte-identity
via a documented re-render.
</content>
</invoke>
