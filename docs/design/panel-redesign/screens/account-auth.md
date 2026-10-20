# Account, Developers, States, sign-in family, Signup, Legal, M-More

Boards: `Account`, `Developers`, `States`, `SignIn`, `Choose`, `Reset`, `Signup`, `Legal` (each with
`T-` and `M-` variants) and `M-More`. Citations refer to **HEAD `b284899`**. Decisions and constraints:
[README](../README.md). Status words: [pass-ledger](pass-ledger.md).

**Binding rules here**

- CLAUDE.md §7: never log or print the API key, the `tt_whsec_` signing key, the webhook URL or
  `*url.Error`. The webhook address is printed only on the owner's panel through
  `egress.Endpoint.ShowToTheOwner()` (`internal/egress/endpoint.go:74-76`).
- §9 / ADR 0023/0024: the tenant accent has two panel slots (4 px stripe, Account preview's tap button);
  the VIES-gated logo is the activation family's only (`GetTenantActivationBrand/Logo`,
  `db/queries/branding.sql:95-128`).
- Shared components that also render tap / activation screens do not change: `components.Notice`,
  `components.Card`, `pages.Problem` (`activate.templ:614`), every `layout.*`
  (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`; `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`,
  layout entries `internal/handler/panelbrand_golden_test.go:138-145`).
- No new JS: only the sign-in page loads `/static/js/adminpassword.js` (`admin.templ:53` passes `adminPasswordScript` to `layout.Auth`, the constant at `:156`; `adminLoginCSP`,
  `adminlogin.go:1912`).
- Tailwind scans templ and `web/static/js` only; no utility names in templ comments.

---

## 0. Shell vs boards (after `0bb5085` / `dfb122d`)

| # | Board | Implemented | Status | Port |
|---|---|---|---|---|
| S1 | sidebar tenant switcher "Kebab Factory / 9 venues · 112 people ⇅" | name only when branded (`panelBrandHeader`) | **REMOVED (D1)** | no in-session switching; counts would be a chrome read on every page |
| S2 | no sign-out in the sidebar foot | `.side-foot` "Signed in as … / Sign out" (`admin.templ:417-428`) | KEPT AS CODE | sign-out must be reachable from 768 px |
| S3 | stripe always on (green when unbranded); green bar on auth pages | stripe only when the theme is linked | KEPT AS CODE | WL-8 golden needs unbranded = no stripe; auth pages may draw a decorative `h-1 bg-tappa-green`, never `.panel-stripe` / `bg-brand` (`TestPanelBrand_TheSignInFamilyStaysTaptime`, `panelbrand_test.go:604`) |
| S4 | kicker + 44 px h1 + right-aligned action | `text-3xl` h1 from `Section.Label` (`admin.templ:433-435`) | **M12-01** | Kicker / Title / Crumbs / Actions slots; `text-3xl md:text-4xl desk:text-[44px]` |
| S5 | phone back arrow, tenant name in the top bar, search icon | porcelain lockup | **REMOVED (D1)** (search, unbranded tenant name) | a back link is a plain link if a page wants one |
| S6 | M-More as a full page | `<details class="bar-more">` sheet (`panel.templ:142-172`) | KEPT AS CODE | user-approved in the shell commit |
| S7 | dark tenant card with Switch | — | **REMOVED (D1)** | |
| S8 | rounded grouped list, 56 px rows, chevrons, dashed separators (the earlier full-page More, `0e31694`) | `.sheet-list` rows `min-h-12 border-b` | KEPT AS CODE | the board (`fd18724` / `c2dd88d`) draws the shipped rows; M12-47's polish = lift shadow (ink/10) + scrim (ink/40) + sign-out radius 10 px. Tapping the scrim closes the sheet (CSS only, no JS) |
| S9 | row badges ("1 to replace", "Owner") | — | **REMOVED (D1)** | |
| S10 | "Manual entry" link | — | **REMOVED (D1)** | a manual entry starts from a person; `/admin/records/new` without `?id=` redirects with a problem |
| S11 | "Signed in as … · Owner" + Sign out | `.sheet-foot` (`admin.templ:440-447`) | EXISTS | button `min-h-12 rounded-xl border border-ink font-bold` |
| S12 | wordmark | same; "t." on the rail | EXISTS | |

---

## 1. Account — `/admin/account`

**Route** `GET /admin/account` (`internal/handler/account.go:65`); `accountSection` (`:157`), `accountView`
(`:192-262`). POSTs `accountSave` (`accountactions.go:106`, owner-only via `mayEditAccount` `:95`),
`accountPasswordSave` (`accountpassword.go:73`, every live admin), brand POSTs (`brandactions.go:63-65`;
upload `brandupload.go`). **Templ** `pages/account.templ` (`AdminAccount` :33, `accountFacts` :67,
`accountVAT` :129, `accountVATMarker` :170, `accountForm` :204, `accountPasswordForm` :318, `accountNoForm`
:407, `accountMessages` :426, `accountBrand` :453, `accountBrandPreview` :496, `accountBrandLogo` :518,
`accountBrandAccent` :560). **Views** `AccountView`, `AccountForm` (`accountview.go:22-187`), `BrandEditor`,
`BrandOutcome`, `PanelBrand`, `PanelLogo`. The Account tab is **not** owner-only — managers read this page.

### 1.1 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Kicker "Your business, as Taptime knows it" | — | DERIVE (S4) | |
| Header "Save changes" (desk) | `accountForm` submit "Save these details" | DERIVE | owner only (`v.CanEdit`); from the header it would need `form=` — **the Account page pins `form=` count 0** (brand preview rule), so the button stays at the bottom of Identity at every width |
| Business name | `Form.Name`, `NameMaxLength`, `DescribedBy("name")` | EXISTS | keep help, error and still-on-file lines |
| VAT number | `v.VATNumber` | KEPT AS CODE | **read-only text** (`<p class="font-mono">`), never an input with a `name` (`accountview.go:27-30`; no UPDATE grant) |
| VAT chip "✓ Verified in VIES · 2 Oct" | `VATState`, `VATStateWord`, `VATCheckedAt` (`fillAccountVAT`, `account.go:314`) | DERIVE | all **four** states; class switch stays in templ (`accountVATMarker`); a short date `VATCheckedShort` in the business zone; keep the full "Last answer …" line as help |
| Kind of business | `v.Types` = `signup.BusinessTypes`, **8** values (`internal/domain/signup/signup.go:174-183`) | EXISTS | all 8 (`TestAccount_TheTypeListIsTheSchemasVocabulary`) |
| Time zone | `<input list=…>` + datalist (`accountview.go:114-118`) | KEPT AS CODE | not a closed `<select>`; keep the holding-lock sentence and the "What the timezone moves" notice; "(CET/CEST)" **REMOVED** |
| Help line | `account-business_type-help` / `account-timezone-help` | DERIVE | "The kind picks the message your staff see after a tap; the message itself can't be written by hand." |
| Manager view of Identity | `accountNoForm` + `WhyNotEdit` | EXISTS — **keep** | `TestAccount_ManagerMayReadAndMayNotSave` |
| "Registered on", "On file" docket, Billing link | `RegisteredOn`, `accountFacts`, `BillingHref` (owner only) | EXISTS — **keep** | `TestAccount_ShowsWhatIsOnFile`, `TestAccount_NeverPrintsTheCommercialTerms` |
| Logo box "PNG or JPEG, up to 512 KB" | `accountBrandLogo`; `LogoMaxKB` (`internal/brand/logo.go:23`) | EXISTS (restyle) | native file input inside a dashed box (no drag-and-drop); `{ itoa(e.LogoMaxKB) } KB`; saved logo shown from `c.Brand.Preview`; keep "Remove the logo"; keep "not a photo of a person" |
| "Shown only when your VAT number is verified." | — | DERIVE (corrected) | **"Your logo shows on the activation screens only once your VAT number is verified."** (tap, result and panel show it regardless) |
| Accent swatches (4 presets) | free colour picker `accent` + hex box `accent_hex` (`account.templ:560-614`) | **REMOVED (D1)** | the picker and hex box stay as the only inputs |
| Hex box | `#brand-accent-hex`, `e.Typed` | KEPT AS CODE | empty on a plain load; the picker carries the saved value (`PickerValue()`) |
| Live preview strip | `accountBrandPreview` (saved accent only) | KEPT AS CODE | restyle the frame only (`flex flex-wrap items-center gap-3.5 rounded-xl bg-porcelain p-3.5`); candidate preview and the "panel strip" sample **REMOVED** (a third accent slot) |
| Contrast verdict box | refused save: `AccentError` + `Suggest` "Use #XXXXXX" (`brandactions.go:385`; `brand.Suggest` `accent.go:240`) | KEPT AS CODE | the board's ratio box maps to the existing refusal + suggestion, restyled `border-dashed border-saffron bg-saffron/10`; a ratio for the saved accent **REMOVED** (needs a new func); "white text" never appears (the gate picks paper **or** ink, `accent.go:182`) |
| "Back to Taptime green" | `HasAccent` reset form | EXISTS — **keep** | |
| Brand outcome notice | `brandOutcomeOf` (`brandactions.go:234`) | EXISTS — **keep** | incl. the light-logo warning |
| Manager brand view | `e.CanEdit=false` → `WhyNotEdit` | EXISTS — **keep** | |
| Developers summary card | `/admin/api-keys` (owner-only) | **DERIVE — owner-only link card only** (D1) | "Integration keys and webhook → Developers", gated by `mayManageAPIKeys`; no key or webhook data on Account |
| Managers card | no query; no scope column; M7-07 blocked | **REMOVED (D1)** | |
| Group / Holding card | `tappa_app` reads no membership (`account_test.go:694-695`) | **REMOVED (D1)** | would need a `holding_*` change (ADR 0027 §2) |
| **Password card** | `accountPasswordForm` (`account.templ:318`) | EXISTS — **keep** | |
| **"What your staff read"** | `accountMessages` (`account.templ:426`) | EXISTS — **keep** | `TestAccount_ShowsTheSentencesThisBusinessesStaffRead`, `TestAccount_SaysTheMessagesCannotBeEdited` |
| Saved / Problem notices | `v.Saved`, `v.Problem` | EXISTS — **keep** | |

### 1.2 Breakpoints and recipes

Card grid `grid grid-cols-1 items-start gap-5 desk:grid-cols-2` (two ~420 px columns at 864). Identity inner
`grid grid-cols-1 gap-4 md:grid-cols-2`, name and kind `md:col-span-2`. Brand card: Logo | Accent side by side
on `md`. Card `rounded-[18px] border border-line bg-paper px-6 py-[22px] flex flex-col gap-4 min-w-0` (phone
`rounded-2xl px-4 py-[18px]`). Field, buttons: README §5 (`.field-stack`, not `.filter-input`).

### 1.3 Copy

"What you can change" → "Identity"; "Save these details" → "Save changes"; "Tap button colour" → "Accent
colour"; logo help shortened but keeps "not a photo of a person" (ADR 0024).

### 1.4 Not ported (Account)

header "Save changes" (needs `form=`) · accent swatches · candidate live preview and the panel-strip sample ·
contrast ratio readout · "white text" wording · "Shown only when your VAT number is verified" · Developers
summary with keys / webhook state · Managers card + "Invite manager" · Group / Holding card · VAT as an
input · closed time-zone select · "(CET/CEST)".

### 1.5 Tests

`internal/handler/account_test.go` (33 tests, `:88-1453`), `accountsection_db_test.go`, `accountpassword_test.go`,
`brandactions_test.go` (`TestBrandPreview_*`, `TestBrandEditor_*`), `brandupload_test.go`, `panelbrand_test.go`,
`panelbrand_golden_test.go`, `TestPanelScreens_FormControlsCarryATouchTargetClass`. Keep: four VAT states,
commercial-terms absence for managers, password form, staff sentences, saved-only preview, owner-only forms,
`TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit` (page `form=` count 0).

### 1.6 Red lines

Account is manager-readable: no key / webhook data, no admin list · §4.5 holding membership needs ADR 0027 §2
approval · §9 the preview is the only accent in the body · CSP: colours are literal classes.

---

## 2. Developers — `/admin/api-keys`

**Route** `GET /admin/api-keys` (`internal/handler/apikeys.go:39`, owner-only); `apiKeysSection` (`:123`),
`readAPIKeysView` (`:170`); POSTs create (`:257`), revoke (`:308`), revoke-all (`:342`); webhook routes
(`webhooks.go:44-50`); `readWebhookView` (`:196`), `readWebhookDeliveries` (`:598`). **Templ**
`pages/apikeys.templ` (`AdminAPIKeys` :21, `apiKeyShownOnce` :56, `apiKeyBudgetAlerts` :80, `apiKeysIntro`
:102, `apiKeyCreateForm` :125, `apiKeysList` :154, `webhookKeyShownOnce` :230, `webhookSection` :250,
`webhookDocket` :293, `webhookDeliveries` :381, `webhookAddressForm` :456). **Views** `APIKeysView`,
`APIKeyRow`, `APIKeyBudgetAlert`, `WebhookView`, `WebhookDeliveryRow` (`apikeysview.go`).

### 2.1 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Kicker "Owner only · keys and the webhook for your payroll or HR system" | — | DERIVE | |
| "Integration guide" header link | guide only on `TAPPA_API_HOST` (`internal/handler/api/guide.go:19,34`; `cfg.APIHost`, `config.go:124`) | DERIVE (as text) | print the address as mono **text** when `APIHost != ""`, nothing otherwise — a relative link 404s, an absolute one fails the panel `://` scan (`dashboard_test.go:563`) |
| Keys h2 + intro | `apiKeysIntro` | EXISTS | board copy accurate (first characters kept); keep "not available on this deployment" and "API access is off" |
| Create form | `apiKeyCreateForm` (only `if v.APIEnabled`) | EXISTS | keep "Two live keys cannot share a name." and the error; `flex flex-wrap items-end gap-2` |
| One-time reveal box | `apiKeyShownOnce`: Notice + readonly `<input id=api-key-new value={v.NewKey}>` | EXISTS (restyle) | `OneTimeSecret` (warn tone); key **exactly once** in the response (`TestApiKeyPanel_TheKeyIsShownOnceAndNeverAgain`, `apikeys_db_test.go:222-276`); "Copy" **REMOVED**; "I've stored it" = plain link to `apiKeysHref` |
| Keys table | `APIKeyRow{Name, PrefixHint, CreatedOn, LastUsed, RevokedOn}`, `Live()` | EXISTS | "Starts with" = `PrefixHint` + "…" (the board's masked **last** 4 is wrong — `token.go:145`); short absolute dates "2 Oct 2026, 14:02"; "Revoked · 3 Oct" in the status cell |
| Status pill Live / Revoked | `k.Live()` | DERIVE | `rounded-full` allowed (D2) |
| Inline revoke confirm | one POST, no confirmation (single key) | KEPT AS CODE | the `<details>` confirm **not ported**; one-click revoke stays |
| "Revoke every key" | `RevokeAllHref` form with required `confirm` checkbox, only `if v.AnyLive()` | EXISTS — **keep** | "Yes, stop every integration now" stays |
| Deactivation-budget alert | `apiKeyBudgetAlerts` | EXISTS — **keep** | |
| Webhook card ON / OFF | `w.Active` | EXISTS | |
| Webhook intro | `webhookSection` | DERIVE (copy) | "We send every punch to one address of yours, signed with a key only you and Taptime hold." |
| Address field (pre-filled) | `w.Address` = `ShowToTheOwner()`; change form `AddressValue` | DERIVE | pre-filling with `w.Address` is allowed (owner panel); never logged; keep "The saved address no longer passes…" and the create form |
| "Sends: every punch …" | — | DERIVE (static) | true (ADR 0028 §4) |
| "Signing key: tt_whsec_••••a21c · rotated 4 Oct" | `RotatedOn` / "Since it was created" | EXISTS (date) | masked key **REMOVED (D1)** (stored sealed) |
| "Created" | `w.CreatedOn` | EXISTS | |
| "Test messages: N of 10 left this hour" | `w.Spent`, `w.Limit` (`endpoints.BucketLimit`), `NextSlot` | DERIVE | `Limit − Spent`; the number from `w.Limit`, never a literal; keep the full-bucket sentence ("used its 10 test messages and resends for this hour", "The next one is free at") |
| Ping result | `PingShown/PingDelivered/PingSentence` (`webhookPingSentence`, `webhooks.go:162`) | EXISTS | exact sentences; "in 312 ms" **REMOVED (D1)** |
| Actions (test, change address, rotate, switch off/on) | separate forms; rotate with required confirm | EXISTS | `flex flex-wrap gap-2`; rotate confirm checkbox kept (`problem=webhook-not-confirmed`); API closed → only "Switch off" |
| Webhook key reveal | `webhookKeyShownOnce` | EXISTS — **keep** | exactly once; keep the X-Taptime-Signature explanation |
| Deliveries header + schedule | `webhookDeliveries` | EXISTS | keep `LogKeepsDays` (90) |
| Deliveries table | `WebhookDeliveryRow`; `CanResend()` | EXISTS / DERIVE | full UUIDs (short ids are sample data; a first-8-hex prefix allowed); **5 states** incl. **Sending** (`webhookDeliveryWord`, `webhooks.go:545`); notes derived ("gave up after N tries", "waiting for its first try", "Arrived"); last answer in the product's words (`webhookAnswerWords`); resend only `d.Resendable && w.CanResend()` |
| State pill colours | — | DERIVE | literal templ switch on a state token |
| Footnote | partly `webhook-bucket-full` | DERIVE | true; "10" from `w.Limit` |
| Newest / Older deliveries | `DeliveriesNewestHref` / `DeliveriesOlderHref` | EXISTS — **keep** | |
| Not configured / unavailable | `!w.Configured`; `problemAPIKeysUnavailable` | EXISTS — **keep** | |

### 2.2 Breakpoints and recipes

Keys card full width; Webhook and Deliveries `grid gap-5 desk:grid-cols-[minmax(0,420px)_minmax(0,1fr)]`
(420 + 444 at 864 — the deliveries table scrolls inside its card). Tables `hidden desk:table`, lists
`desk:hidden`. Phone: create form stacked, Revoke full width, actions "Send a test message" full width + a
2-column row. Key row, one-time box, delivery pill: README §5.

### 2.3 Not ported (Developers)

Copy buttons · masked `tt_whsec_` tail · "in 312 ms" · short delivery ids as the only id · inline revoke
confirmation · an absolute guide link · 4-state delivery legend.

### 2.4 Tests

`apikeys_db_test.go` (exactly-once key, hint afterwards, manager 403, CSRF), `apikeys_test.go`,
`webhooks_db_test.go` (ping sentences, bucket sentence, the ping form disappears when the bucket is full,
deliveries + resend), `webhooks_test.go` (`TestWebhookPanel_EveryClassHasASentence`), the panel `://` scan,
the WL-8 golden, `TestWebhookPanel_TheKeyIsShownOnceAndNeverAgain`, `TestWebhookPanel_PingResultsAndTheBucket`,
`TestWebhookPanel_AClosedAPIOffersNothingButTheSwitchOff`, `TestWebhookPanel_NotConfiguredSaysSo`,
`TestApiKeyPanel_AClosedAPIOffersNoCreateAndStillRevokes`, `TestApiKeyPanel_TheDeactivationBudgetAlertIsShownToTheOwner`,
`TestApiKeyPanel_ManagerIs403EverywhereAndLeavesATrail`; leak tests `TestEndpoint_PrintsAPlaceholderEverywhere`,
`TestWebhookWorker_URLAndURLErrorNeverReachLogOrAudit`, `TestApiKeyAuth_NothingLogged`.

### 2.5 Red lines

§7: no new log line, title, `data-` attribute or second rendering of the key or the secret; no invented
masked secret; the address only via `ShowToTheOwner`.

---

## 3. Empty and problem states — `States` (reference board)

Sources: `components.EmptyState` (`web/templates/components/panel.templ:278-286`, `.empty-state`
`input.css:426-429`), `components.Notice` (`notice.templ:40-50`), `(*AdminAuth).renderProblem` →
`pages.Problem` (`internal/handler/adminlogin.go:1757`; `activate.templ:614`, `layout.Page`; 161 call sites).

| Element | Source | Status | Port note |
|---|---|---|---|
| Empty card (eyebrow = section name, dashed ink/50, `rounded-[18px]`, optional CTA) | `EmptyState(heading)` (eyebrow "Nothing here yet", dashed `border-line`, square) | DERIVE — **M12-09** | new `PanelEmpty(where, heading, cta)`; callers: transactions :71, reports :224, locations :189/215/263/295, review :128, anomalies :205/374, employees :247, admin :516; holdingless goldens re-rendered in a dedicated commit |
| Spike empty | `review.templ:128` | KEPT AS CODE | |
| Pass empty "Nobody tapped … between 06:00 and 06:00" | `emptyHeading` | KEPT AS CODE | times **REMOVED** (calendar day) |
| People empty "No one invited yet" + "Invite someone" | `emptyRosterHeading`: "Nobody on the books yet" / filters / paged | KEPT AS CODE | CTA "Add somebody" → `?add=new`; keep the filter and paged variants |
| Venues empty "No plaques are mounted" + "Order plaques" | `locations.templ:263` | KEPT AS CODE | CTA **REMOVED** (D1) |
| Roll empty | `reports.templ:224` | EXISTS | |
| Odd empty | `anomalies.templ:374` | EXISTS | |
| Notices, 3 tones with a label word | `components.Notice` (left border, no label) | NEW — **M12-09** | `PanelNotice(tone, heading)`; **do not edit `Notice`** |
| Problem card (code chip, OWNER ONLY pill, CTA) | `pages.Problem` in `layout.Page` | NEW — **M12-10** | `PanelProblem`; `renderProblem` switches; `pages.Problem` bytes unchanged; unauthenticated problems → CTA `/admin/login` |
| 404 "Not found" | chi's default plain 404 | **not ported** | a panel NotFound handler is new routing (backlog) |
| 401 "Sign in again … you will come back here" | session loss = 303 to `/admin/login` (`requireLogin`, `adminlogin.go:766-771`) | **REMOVED (D1)** | there is no 401 page; a return URL is an open-redirect surface |
| 403 "Only the owner can do that — Managers can read this…" | per-problem copy (e.g. `problemAPIKeysOwnerOnly`) | KEPT AS CODE | Billing / Developers managers cannot read either |
| 503 "… Your taps are still being recorded." + Try again | `problemPanelUnavailable` and per-section unreadable problems | DERIVE | the "taps are still being recorded" sentence **REMOVED** (the panel cannot know) |

Grid for the reference board only; within a page there is one empty state, full width. Problem card `max-w-xl`.
Tests: holdingless goldens (pages + handler), every test that greps "Nothing here yet" (grep first),
`TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy` (problems moving into the shell get the panel CSP),
`TestAdminScreens_CarryTheSecurityHeaders`, tests asserting a problem body
(`TestAccount_AFailedReadIsAProblemPageAndNeverABlankForm`, `TestApiKeyPanel_ManagerIs403EverywhereAndLeavesATrail`).

---

## 4. Sign in — `/admin/login`

`GET`/`POST /admin/login` (`adminlogin.go:515`); `renderLoginScreen` (401 on failure, `:1429`). Templ
`AdminLogin` (`admin.templ:52-182`), `adminPasswordToggle` (`:186`), `adminLoginBrandPanel` (`:237`), unused
`adminLoginPlaque` (`:278`); shell `layout.Auth(title, adminPasswordScript)` (`layout/base.templ:495`). View
`AdminLoginView{CSRFToken, Failed, ResetHref, Recovered, SignupHref, FreeMonths}`, `AdminLoginProof`.

| Element | Source | Status | Port note |
|---|---|---|---|
| Split layout | `flex min-h-screen flex-col lg:flex-row` | EXISTS (restyle) | `desk:flex-row`; tablet band on top |
| Wordmark | green ring mark + "taptime" link to `/` | DERIVE | stays a link to `/` (`TestAdminLogin_ChromeLinksReachRoutesThatAreMounted`) |
| Decorative dockets on a rail | none | NEW (static, `aria-hidden`) | `.docket` + `.stamp stamp--approved`; literal tilt classes; hidden below `md` (phone: one mini docket); made-up names allowed on a decorative element (landing precedent) |
| Headline "Punchless time & attendance." + paragraph (`text-line` on ink, 10.62:1) | "Every shift starts / with a tap." + `AdminLoginProof` (line 3 stale: practice tap) | DERIVE — **M12-04 first** | new sentences ("judged, stamped, kept" — true); rewrite `TestAdminLogin_BrandPanelClaimsOnlyWhatTheProductDoes` (`adminloginsplit_test.go:303-358`) to anchor them, keeping its free-months assertion and banned phrases |
| "Not using Taptime yet? Start now — first N months free →" | `v.SignupHref`, `v.FreeMonths` | EXISTS | constant-bound number |
| Card "Welcome back" | same | EXISTS | `rounded-[18px] … px-9 pt-9 pb-8` (phone no card) |
| Recovered notice | `v.Recovered` + `Notice(ToneOK)` | EXISTS | `PanelNotice` allowed (auth family is not in the unbranded golden) |
| Failure notice | `v.Failed` | EXISTS — **binding** | names no cause (`TestAdminLogin_FailureBodyNamesNoCause`, `TestAdminLogin_ThreeFailuresAreByteIdentical`, `TestAdminLogin_RefusedSignInStillRendersTheFailureNotice`; `op8_db_test.go:471`) |
| E-mail / password fields | ids `admin-email`, `admin-password`, `autocomplete`, `required`, `autofocus` | EXISTS | `TestAdminLoginPassword_FieldKeepsEveryAttributeAPasswordManagerReads`; drop the "••••••••" placeholder |
| Password reveal | `adminPasswordToggle` + `adminpassword.js` (ships `hidden`) | EXISTS | keep `hidden`, `data-pw-toggle`, `aria-controls`, the two icons; `TestAdminLoginPassword_OnlyTheSignInPageWidensIt` |
| "Sign in" | `btn btn--primary w-full` | EXISTS | |
| "Forgot password?" | `v.ResetHref` | EXISTS | drawn only when reset is wired (`TestAdminLogin_OffersNoSignInLinkItCannotSend`) |
| Footer "This is the manager panel. Employees do not sign in — they tap the plaque." | same | EXISTS | |
| board state toggle | — | **REMOVED** | |

Breakpoints: phone brand band `px-5 pt-[22px] pb-[26px]`, form `px-4 py-6`, button `min-h-[54px]`; `md` band on
top, form card max 440; `desk` split. Risks: script set unchanged; form action `/admin/login`, CSRF field
`csrf` (`TestAdminLogin_SplitLayoutKeepsTheFormItPosts`); `layout.Auth` bytes unchanged.

---

## 5. Which business? — `/admin/login/choose`

`GET`/`POST /admin/login/choose` (`adminlogin.go:526-527`); `AdminChoose` (`admin.templ:305-330`) in
`layout.Page`; `AdminChooseView{CSRFToken, Businesses []AdminBusiness{TenantID, TenantName, Role}}`.

| Element | Source | Status | Port note |
|---|---|---|---|
| Page chrome | `layout.Page` | DERIVE | move to `layout.Auth(title, "")` (script empty; `adminCSP` kept; update the "one call site" comment) |
| Kicker | — | DERIVE (corrected) | **"Password accepted · one more step"** (no session exists yet — "Signed in" is wrong) |
| h1 "Which business?" + paragraph | same | EXISTS | |
| Rail + hanging dockets | `.docket` per business form | DERIVE | tilt by `nth-child` (or index in templ, literal classes); clip `w-[46px] h-[18px] bg-ink/70 rounded-b-md`; grid `grid gap-6 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]` |
| Docket: business, role, button | `TenantName`, `Role`, hidden `tenant_id`, `csrf` | EXISTS | button "Continue to {name}" at every width (unique accessible names); field names kept |

Tests: `adminlogin_test.go:1749-2433` (choose), `TestPanelBrand_TheSignInFamilyStaysTaptime`,
`TestAdminScreens_CarryTheSecurityHeaders`.

---

## 6. Password reset — `/admin/reset`, `/admin/reset/new`

Routes (`internal/handler/adminreset.go:306-307`); templ `AdminResetRequest` (`adminreset.templ:30`),
`AdminResetSent` (`:74`), `AdminResetNew` (`:112`), all in `layout.Page`; views
`AdminResetRequestView{CSRFToken, CanDeliver, SignInHref}`, `AdminResetSentView`, `AdminResetNewView{CSRFToken,
MinPasswordChars, Error}`.

| Element | Source | Status | Port note |
|---|---|---|---|
| Chrome + "← Back to sign in" (header) | link at the bottom of request/sent; none on `/new` | DERIVE | `layout.Auth(title, "")`; add `SignInHref` to `AdminResetNewView`; the link is a plain `/admin/login` (no token in any href) |
| Step nav (3 steps) | — | DERIVE (static) | `<ol>` with `aria-current="step"`; **not clickable**; subs accurate ("The link works once, for an hour", `TestAdminResetConstants_ShippedValuesArePinned`); phone "step N of 3" |
| Step 1 "Can't get in?" + field + "Send me a link" | `AdminResetRequest` | EXISTS | |
| No-mail note | `!v.CanDeliver` + warn notice | EXISTS — **copy fixed in M12-05** | the "Ask another owner … from the panel" sentence **REMOVED** (no such flow) |
| Step 2 "Check your email" | `AdminResetSent` | EXISTS — **binding** | byte-identical for registered / unregistered (`TestAdminReset_RegisteredAndUnregisteredAreByteIdentical`, `TestAdminReset_TimingIsFlat`); no echo of the address |
| "Mockup: open the link from the email →" | — | **REMOVED** | |
| Step 2 "Nothing was sent" | same | EXISTS | same copy fix (M12-05) |
| Step 3 "Set a new password", "At least N characters", fields, error notice, footnote | `AdminResetNew`; `password` / `password_confirm`; `v.Error` | EXISTS | `itoa(v.MinPasswordChars)`; keep the error notice; `TestAdminResetNew_TakesTheTokenOutOfTheURL` |

Breakpoints: `desk:grid desk:grid-cols-[260px_minmax(0,520px)]`; below desk the nav collapses into the step
header. Tests: `adminreset_test.go` (23 tests, `:273-1544`), `resetlivelink*`, `TestPanelBrand_TheSignInFamilyStaysTaptime`.

---

## 7. Signup — `/signup` …

Routes `/signup`, `/signup/places`, `/signup/account`, `/signup/done` (`internal/handler/signup.go:24-27`);
CSP `signupCSP` (`:953`, no script). Templ `signupShell` (`signup.templ:44`), `fieldError` :75,
`SignupBusiness` :85, `structureCard` :209, `SignupPlaces` :233, `SignupAccount` :283, `SignupDone` :387,
`SignupProblem` :486; chrome `marketingChrome` (`landing.templ:578-615`). **Three steps** + done.

| Element | Source | Status | Port note |
|---|---|---|---|
| Header + "Already with Taptime? Sign in" | `marketingChrome("")` — **empty href** | DERIVE — **M12-06 first** | pass the sign-in path; prefer a parameter or a signup-specific header (Legal and Landing share the chrome); "sign in" must stay anchored in the signup claims vocabulary |
| Progress docket (business name, 3 steps, APPROVED stamp when done, "Nothing is created until the last step…") | "Step N of 3" label only | DERIVE | name from the views (`BusinessName` / `Name`; first visit "New business"); marks per README step progress; hidden or "stopped" on problem screens |
| Card eyebrow, h1, blurb | `signupShell` | EXISTS | |
| Business name, VAT, help | same | EXISTS | |
| Kind-of-business chips (8) | native radio + `peer` (`<input type=radio class="peer sr-only">`) | EXISTS (restyle) | `peer-checked:border-2 peer-checked:border-tappa-green peer-checked:bg-tappa-green/10`, `rounded-full` or `rounded-[10px]`, `min-h-11`; labels = `signup.BusinessTypes` |
| One / Several cards | `structureCard` | EXISTS | `rounded-[14px] min-h-24 p-4` |
| Step 2 place rows | `v.Venues`, `MaxVenues` | EXISTS | live row-adding **REMOVED**; "25" from `v.MaxVenues` |
| Step 3 two columns + "Back" | `SignupAccount` | DERIVE | `grid gap-6 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]`; Back = `<a href="/signup/places">`; keep the **honeypot** exactly (`TestSignup_TheHoneypotFieldIsOnTheFormAndHidden`) |
| Step 3 "…You can invite the rest of your managers later." | current templ | **REMOVED — M12-07** | manager invites are blocked (M7-07 / Q02) |
| Done: docket + VAT note box + "What happens next" + "Sign in to your dashboard" | `SignupDone`, `VATNote()`, `SignInBlocked` | EXISTS (restyle) | VAT tone by literal class per bool; keep the `SignInBlocked` alert; plaques wording "loads it into your dashboard" stays (`TestSignupSurface_MakesNoDeliveryClaim`, `TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck`) |
| "Replay the wizard" | — | **REMOVED** | |
| Problem screens | `SignupProblem`, `v.Retry` | EXISTS (restyle) | alert recipe; the four problem constants |
| Footer legal nav | `marketingChrome` footer (biometric / GPS paragraph + 4 links) | KEPT AS CODE | keep the claim paragraph |
| Phone progressbar | — | DERIVE | `role=progressbar`, `aria-valuenow` |

Breakpoints `desk:grid desk:grid-cols-[minmax(0,360px)_minmax(0,680px)] gap-10`; tablet aside above the card;
phone progress bar. Tests: `signup_test.go` (25 tests, `:252-1409`; capability and delivery scans over every
signup screen), `TestSignupCSP_NamesWhatThePageLoads`, `TestSignup_EveryScreenIsUncacheableAndCarriesThePolicy`,
`TestSignup_ARefusedValueIsNotEchoedRaw`, `TestSignup_ServerRefusesABadAccountAndKeepsThePasswordOffTheScreen`;
`marketing_test.go` if the chrome changes.

---

## 8. Legal — `/legal/*` (public page, marketing chrome — not a panel screen)

Routes `/legal/privacy`, `/legal/terms`, `/legal/imprint`, `/legal/cookies` (`pages.LegalPages`,
`internal/handler/marketing.go:300-311`; `legal` `:337-349`; `marketingCSP` `:466`; public `Cache-Control`).
Templ `Legal` (`legal.templ:37`), `legalBody` :72, `legalDraftNotice` :95, `legalCookieTable` :123. Views
`LegalPageView{Page, SignInHref, Body, PublishedAt, Cookies}`, `CookieRow` (`landingview.go:925-1071`); rows
from `cookieNotice()` (`marketing.go:640`).

| Element | Source | Status | Port note |
|---|---|---|---|
| Header: wordmark + Sign in + "Start free" | `marketingChrome(v.SignInHref)` | DERIVE | add `SignupHref` to `LegalPageView` (the handler has `m.signupHref`); nothing when signup is off; prefer a chrome parameter |
| Doc nav with "NOT IN FORCE YET" chips | `m.texts.Published()` (`marketing.go:342`) | DERIVE | links to the four routes, `aria-current="page"`; pass `PublishedSlugs`; chip only when not published; same bytes for every visitor (caching valid) |
| Eyebrow, h1, lede | same | EXISTS | |
| Published body + date | `legalBody` | EXISTS — **keep** | |
| Cookie table | `legalCookieTable(v.Cookies)` | EXISTS (restyle) | flags stay data (`{ c.Flags }`); a shared-flags footnote only via an `AllFlags()` helper, else per row; rows never hard-coded (11 today) |
| Phone cookie cards | — | DERIVE | `md:hidden` cards + `hidden md:block` table; keep `<caption class="sr-only">` |
| Draft notice "Waiting on" 01… | `legalDraftNotice` | EXISTS (restyle) | numbers from the index |
| Footer | `marketingChrome` | KEPT AS CODE | keep the claim paragraph |

Breakpoints `desk:grid desk:grid-cols-[minmax(0,300px)_minmax(0,820px)]`; tablet nav as a row; phone 2-column
chips. Tests: `marketing_test.go` (`TestMarketing_MountsEveryLegalDocumentAndLinksToIt` `:701`,
`TestLegalPages_SkeletonsAreNotIndexedAndSayWhatTheyAreWaitingFor` `:1461`, `TestMarketing_ReachesNoThirdParty`
`:509`), `legalpublic_test.go`, `marketing_claims_test.go`, `marketing_facts_test.go`, the
`TestCookieNotice_*` family.

---

## 9. M-More sheet

Keep `<details class="bar-more">` and its shipped rows (S8 — the board draws them); CSS-only polish: the sheet
lifts on an ink/10 shadow, the page above the bar is dimmed at ink/40, the sign-out button takes the 10 px
radius. The scrim is a `::before` of More's own `<summary>`, so tapping it closes the sheet — CSS only, no JS;
the bar stays lit and pressable. No badges (S9), no tenant card (S7), no Manual entry link (S10). Keep `aria-current` and the sr-only ", current: X"
(`dfb122d`) and `TestPanelSections_ThePhoneNavMarksExactlyOneCurrentAndOffersEverySection`.

---

## 10. Not ported (this group)

tenant switcher and counts · Switch card · sidebar without sign-out · always-on stripe · phone search / tenant
name · M-More full page, badges and Manual entry link · Account items in §1.4 · Developers items in §2.3 · 404
page · 401 card · "Your taps are still being recorded" · "06:00 and 06:00" · "Order plaques" CTA · "Signed in ·
one more step" · clickable reset steps · reset "Mockup" button · signup live row-adding and "Replay the wizard".

## 11. Red lines

§9 sacred screens — new components only · §7 no key / secret / URL leak, no masked secret · §4.5 holding
membership display needs ADR 0027 §2 · owner-only data never on manager-readable Account · CSP — tilt, swatch
and tone are classes · stale copy fixed first (M12-04 … M12-07).
</content>
</invoke>
