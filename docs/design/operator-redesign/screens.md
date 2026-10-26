# Operator screens — element lists for the port

Companion to [README.md](README.md). Per screen: what the code draws, the binding copy, every
form with its hidden fields, the states, what is never shown, and the tests that hold it. Under
D1 a board may show **only** what is listed here.

Citations: written at HEAD `022ba3c` (operator files unchanged since `1e5cc83`; the board-polish
lines of 2026-10-20 re-read at `a8a06f6`, unchanged at `27516b7`). **Re-read on 2026-10-25 at
`98546448`** (the OP-15 annex): every citation into a file that changed since `022ba3c`
(`render.go`, `routes.go`, `surface.go`, `tenants.go`, `audit.go`, `view.go`, `tenants.templ`,
`legal.templ`, `layout/base.templ`, `input.css`, `internal/db/operator.go`,
`internal/domain/legal/legal.go`, `op8_test.go`, `op14_test.go`, `op12_db_test.go`,
`typepins_test.go` — the README header's list) points at
`98546448`, and the OP-15 lines are new (`suspension.go`, `op15_test.go`, `op15_db_test.go`); the
other cited files are byte-identical at both commits (README header lists them). File prefixes:
templates are `web/templates/operatorpages/`, handlers `internal/handler/operator/`, tests
`internal/handler/operator/` unless named. Times print as `utcStamp` "2006-01-02 15:04 UTC"
(`legal.go:344`) unless noted; the audit log uses `utcSecond` "2006-01-02 15:04:05 UTC"
(`audit.go:434`); billing prints in the month's own zone.

Shared vocabulary: `op-card` = paper card; `docket` = perforated paper list (sharp, D2);
`docket-label` = 10 px mono small caps label; `op-link` = tappa-green underlined bold link,
44 px; `btn btn--primary` = green button; `btn btn--quiet` = paper button with line border,
tomato on hover; `op-pager` (new, README §6) = outlined paper button for the Billing, Audit and
Holdings pagers; `op-step` / `op-heading` (new, README §6) = in-card display headings where the
code writes `docket-label` today (Enroll's steps, "Create a holding"). Notices were `components.Notice(tone, heading)`
(`web/templates/components/notice.templ:40-49`) until M13-03 and are the operator-only `opNotice`
(`operatorpages/notice.templ`) with the **same tone and the same words**.

---

## 0. The shell (M13-01)

| Element | Today | After OP-1 |
|---|---|---|
| Band | `bar(signedIn)` `chrome.templ:33-46`: `<header class="op-bar">`, inner `max-w-3xl`, lockup `<p class="op-lockup">TAPTIME <span class="text-saffron">OPERATOR</span></p>`; signed in: `<form method="post" action="/operator/logout"><button type="submit" class="op-signout">Sign out</button></form>` | same markup; inner row full width; one sign-out form at every width |
| Nav | none (`chrome.templ:19-21`) | `<nav class="op-nav" aria-label="Operator sections">` — label "PLATFORM" (desk only); Console `/operator` · Tenants `/operator/tenants` · Holdings `/operator/holdings` · Audit log (phone: "Audit") `/operator/audit` · Legal texts (phone: "Legal") `/operator/legal`; one current, `aria-current="true"` (the section — README §2 "OP-15 annex" (b)) |
| Tenant banner | `tenantBanner(name)` `chrome.templ:54-66`: `<section class="op-tenant" aria-label="Tenant">`, `docket-label` "Tenant", name `<p class="font-display text-xl font-bold tracking-tight"><bdi>…</bdi></p>`; unnamed: `<p …>Unnamed tenant</p>` + `<p class="break-all font-mono text-sm">id</p>` (`view.go:85`) | same markup, in the content column, left-aligned; the name at 26 / 30 / 34 px by a parent rule (README §6 "Tenant banner") |
| Tenant sub-nav | none | `<nav class="op-subnav" aria-label="This tenant">` **inside** `section.op-tenant`, after the name: Overview · Plaques · Billing · VAT number, current saffron 3 px marker and `aria-current="page"` (the page — README §2 "OP-15 annex" (b)); phone 14 px, wraps |
| Main | `<main class="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-8">` (`chrome.templ:72,83,95`) | `<main class="op-main …">`, `max-w-[960px]`, left-aligned in the column (no `mx-auto`, the boards), the boards' padding (README §6 "Content column") |
| Title | `screen`: `<Title> — Taptime operator`; `tenantScreen`: `<Title> — <name or "Unnamed tenant <id>"> — Taptime operator` | unchanged |

DOM order (signed in): `header.op-bar` → `div.op-frame` → `nav.op-nav` → `div.op-col` → [`section.op-tenant` containing `nav.op-subnav`] → `main.op-main`. The banner stays outside and before `<main>` (README §2). The phone sign-out is the same single text button (no icon, no `<svg>`).

Phone bar (polished boards): **sticky** at the viewport bottom, and on a short page it rests on
the viewport's bottom edge — `div.op-frame` is a flex column at least the viewport tall (minus
the band), the nav takes `order-last mt-auto` (README §6 "Frame", "Main nav"). One look and one
padding scale on every operator screen: the perforated `.docket` (no dashed-top shortcut) at
18 px 24 px on tablet/desktop and 16 px 20 px on the phone; a card 20 px all round on the phone
— no per-screen padding (README §6 "Card", "Docket list").

Signed-in shell (band with sign out + nav): Home, every data screen, signed-in problems.
Signed-out shell (band only): SignIn, Code, Enroll (enrollment *ends* in a session —
`chrome.templ:78-79`), signed-out problems.

Which item is current: Console (Home) · Tenants (Tenants, TenantOverview, TenantPlaques,
TenantBilling, TenantVAT) · Holdings · Audit log · Legal texts — marked `aria-current="true"` · none
(signed-in Problem: no `aria-current` in `op-nav`, no marker at any width — as the polished
`OpProblem` boards draw it). Sub-nav current (`aria-current="page"`): Overview / Plaques / Billing /
VAT number. A suspended tenant's screens mark the same items (the suspension is not a section).

**Tests:** `TestOperatorScreens_EveryOneWearsTheOperatorChrome` (extended, README §4.1),
`TestTenantScreen_RefusesToRenderWithoutAName`, `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`,
`TestEnrollQR_OnlyOnTheFirstLoad` (SVG count, extended), `TestOperatorChrome_TheColouredTextClearsAA`
(extended with the nav's ink/70 on paper and ink on ink/5 over paper),
`TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell`,
`TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` (`TenantScreen` stays exported;
`opNav`, `opSubNav` are not).

---

## 1. OpSignIn — `SignIn` (`signin.templ:18-50`) and `Code` (`signin.templ:58-89`)

Signed-out shell. Title `Sign in` / `Sign-in code`.

**SignIn copy**

| Element | Words |
|---|---|
| h1 | Operator sign-in |
| sub | Taptime staff only. This is not the business panel. |
| warn notice (Expired, 401 after the code step's challenge expired) | **That sign-in step expired** — Sign in again with your email address and password. |
| alert notice (Failed, 401) | **We could not sign you in** — Check the email address and the password, then try again. |
| field labels | Email · Password |
| button | Continue |

**Form** `POST /operator/login`: `email` (type email, `autocomplete="username"`, required,
autofocus), `password` (type password, `autocomplete="current-password"`). No CSRF field — the
cookies are SameSite=Strict on the operator's own host and the origin gate checks
(`signin.templ:13-16`).

**Code copy**

| Element | Words |
|---|---|
| h1 | Enter your code |
| sub | Type the six-digit code your authenticator app shows for Taptime operator. |
| alert (Rejected, 401) | **That code was not accepted** — Check that your phone's clock is right, then type the next code. |
| alert (Locked, 401) | **Sign-in is locked for a while** — Too many wrong codes. Wait a quarter of an hour, then try again. |
| field label | Code |
| button | Sign in |
| link | Start again → `/operator/login` |

**Form** `POST /operator/login/totp`: `code` (text, `inputmode="numeric"`,
`autocomplete="one-time-code"`).

**States:** sign-in default · Expired · Failed · code default · Rejected · Locked. Throttled → 429
problem, fault → 503 problem (§4).

**Binding:** the Failed answer names no cause and does **not** echo the typed address (unknown
address, wrong password, pending and disabled accounts answer byte-identically).

**As built (M13-04):** one `op-card` at most 480 px in the signed-out column (README §6
"Signed-out card"); the words, the two forms, their fields and the six states as above; no
hidden field, no `value` on any field.
**Not shown:** forgot password, sign-up, remember me (none exist; a reset is the owner's
`cmd/opadmin` new setup link).

**Tests:** `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike`
(`internal/operatorauth`), `TestSignIn_ReadsTheBodyNeverTheQuery`,
`TestSignIn_TheCodeStepClearsTheChallenge`, `TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes`,
`TestSignInScreens_AreOneCardInTheSignedOutColumn` (M13-04: the column, the card, no top margin in
the card, one form, the fields without a value, the links, no recovery or sign-up word; the
signed-out problem pages in the same column).

---

## 2. OpEnroll — `Enroll` (`enroll.templ:31-107`), script `enroll.js`

Signed-out shell via `scriptedScreen` (the only page with a script). Title `Set up your account`.

| Element | Words / markup |
|---|---|
| h1 | Set up your operator account |
| sub | This link works once, for 30 minutes from when it was made. |
| step 1 heading (first load only) | Step 1 · Add this key to your authenticator app — `<p class="docket-label">` until M13-05; the polished boards draw it as a **display heading, 17 px (16 px phone), bold** → **as built (M13-05):** `<h2 class="op-step">`, same words (README §6). |
| QR (first load only) | `<svg class="op-qr" role="img" aria-label="Scan with your authenticator app" viewBox=… width/height=whole px shape-rendering="crispEdges"><title>Scan with your authenticator app</title><rect fill="#FFFDF4"/><path fill="#152219" d=…/></svg>` (`enroll.templ:40-44`, `qr.go`; ~207–264 px, `qr.go:29`). Omitted if encoding fails — the text key still shows. The boards draw a real sample of this shape (repo encoder, version 8, 57 modules × 4 px = 228 px — the size a host of up to 18 bytes gets, `qr.go:26-28`); its content is sample data. |
| key | Scan the code with your app, or type this key: → `<p class="op-key">` grouped base32 |
| URI | If your app takes a setup link instead, use this one: → `<p class="op-uri">` the `otpauth://` URI |
| warn (re-render, no key) | **Use the key you already added** — Type a code from the authenticator entry this page gave you. If you did not add it, open your link again for a new key. |
| alert Mismatch | **The two passwords differ** — Type the same new password in both fields. |
| alert WeakPassword | **Choose another password** — Use at least 14 characters and at most 72 bytes. A letter with an accent, or from another alphabet, takes two bytes or more. |
| alert CodeRejected | **That code was not accepted** — Check that your phone's clock is right, then type the next code. |
| step 2 heading | Step 2 · Choose a password and confirm the code — `<p class="docket-label">` until M13-05 → `<h2 class="op-step">` like step 1, the form's first child |
| fields | Link code (help: The part of your link after the # sign.) · New password · New password again · Code from the app |
| button | Finish setup |

**Form** `POST /operator/enroll`: hidden `id` (account uuid), hidden `blob` (sealed pending TOTP
key); `<div id="enroll-token-wrap" class="op-token-field">` holding `token` (`id="enroll-token"`,
type password, autocomplete off) — the script fills it from the URL fragment, strips the
fragment and hides the wrapper; without JS it stays visible for pasting; `password`
(minlength 14, `new-password`), `password_again`, `code`.

**States:** first load with JS (token field hidden) · first load without JS (Link code visible) ·
re-render "Use the key you already added" with Mismatch / WeakPassword / CodeRejected. Problems:
"This setup link is incomplete" (400, GET without a valid id), "This setup link does not work"
(400, bad / used / expired link on POST), 429, 503.

**Binding:** the QR, key and URI appear **only** on the first load; the page carries exactly one
`<svg>` (the QR); the token travels in the fragment, never the query.
**Tests:** `TestEnrollQR_IsTheKeysURIInkOnPaper`, `TestEnrollQR_OnlyOnTheFirstLoad`,
`TestEnrollScript_IsTheReviewedBody`, `TestEnrollScript_TouchesTheFragmentAndNothingElse`,
`TestEnroll_TheWeakPasswordNoticeNamesBothLimits`, `TestEnroll_TheTokenNeverTravelsInTheURL`,
`TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll`, `TestEnrollScreen_IsOneCardInTheSignedOutColumn`
(M13-05: the column, the card, its words in order, the step headings, every input's whole attribute
set — the hidden fields' and the token's value by presence only — the labels, the one button and the
one script).

---

## 3. OpHome — `Home` (`home.templ:20-48`)

Signed-in shell, current = Console. Title `Console`.

| Element | Words |
|---|---|
| label | Operator console |
| h1 | You are signed in |
| list | **Tenants** — find a business, and see what it has live · **Legal texts** — Taptime's privacy policy, terms, company details and cookie notice · **Holdings** — group businesses that work in one timezone, and see who is in each · **Audit log** — every sign-in, read and publication on this surface |
| footer | A session ends after 30 minutes without use, and after 8 hours in any case. Sign out when you are done. |

Layout (polished boards = the code's shape): **one `op-card`** (`home.templ:22-47`) — mono label,
`<h1>` 28 px at every width, the four link + gloss pairs as a **two-column grid** on
tablet/desktop and **stacked 44 px links** on the phone, the session sentence as the card's
footer **inside the card** (README §6.1 "Home hub"). Not a docket.

Links: `/operator/tenants`, `/operator/legal`, `/operator/holdings`, `/operator/audit`, each
`class="op-link"`. **No icon tiles:** an earlier board drew a 48 px icon tile beside each link;
the tiles were removed from `OpHome` / `T-OpHome` / `M-OpHome` and are not ported (D1 — Home has
none, and the only SVGs a signed-in page may carry are the nav's five). The order on Home stays Tenants, Legal texts, Holdings, Audit log (the nav's
order is OP-1's).

**Not shown:** the operator's name or e-mail (no definer reads an operator's e-mail,
`home.templ:18-19`); any count, tenant data, recent activity or widget (Home makes no read). No
empty or error state.
**Tests:** `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` pins
`<a href="/operator/holdings" class="op-link">Holdings</a>` and
`<a href="/operator/audit" class="op-link">Audit log</a>` (the nav must use other classes so
these stay exact and unique in meaning). `TestHomeScreen_IsOneCardOfFourLinksAndReadsNothing` (M13-06)
pins the one card, its words in order, the label and the `<h1>` tags, the four rows and their links, nothing
hidden, no form, field or drawing in the column, and that `GET /operator` costs the store only the session's
`TouchOperatorSession`.

---

## 4. OpProblem — `Problem` (`home.templ:53-65`), constants `render.go:137-503`

Alert notice with `<h1>` = Title, `<p>` = Message; optional `op-link` Back / BackLabel; shell
signed in or not per constant. Signed in: band + nav with **no current item** and no tenant
banner or sub-nav; signed out: band only, no nav. All constants are binding (`problemPages()` `render.go:507-524`;
`TestProblemPages_LinkOnlyToMountedRoutes`, `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo`).

| Constant | Status | Title | Message | Back |
|---|---|---|---|---|
| problemTooMany(false/true) | 429 | Too many requests | Wait ten minutes, then try again. | — |
| problemCrossOrigin | 403 | That request did not come from this site | Open the page again and send the form from there. | Go to the sign-in |
| problemFormTooLarge | 413 | That form was too large | Open the page again and send the form from there. | — |
| problemBadForm | 400 | That form could not be read | Open the page again and send the form from there. | — |
| problemUnavailable | 503 | Sign-in is not available right now | Try again in a few minutes. | Go to the sign-in |
| problemEnrollIncomplete | 400 | This setup link is incomplete | Open the whole link from your message, including the part after the # sign. | — |
| problemEnrollRefused | 400 | This setup link does not work | It may have expired (a link lasts 30 minutes) or been used already. Ask for a new one. | — |
| problemSignOutFailed (signed in) | 503 | Sign-out did not finish | Your session is still open. Try signing out again in a moment. | Back to the console |
| problemSignOutThrottled | 429 | Too many sign-out requests from your network | This browser no longer holds your session, but the server could not be asked to end it. It ends after 30 minutes without use; if someone else may hold it, ask for it to be revoked. | Go to the sign-in |
| problemLegalTooLarge | 413 | That text is too long | A document can be at most 256 KiB. Nothing was published. | Back to the legal texts |
| problemLegalUnknownDocument | 400 | That document does not exist | The form named a document Taptime does not publish. Nothing was published. | Back to the legal texts |
| problemLegalEmpty | 400 | There is no text to publish | The box had no visible text — spaces, line breaks and invisible characters do not count. Nothing was published. | Back to the legal texts |
| problemLegalNotPublished | 503 | The publication was not confirmed | The database did not confirm it. Check the version list before you publish again — it shows whether this text went through. | Back to the legal texts |
| problemLegalUnreadable | 503 | The legal texts could not be loaded | Try again in a moment. | Back to the console |
| problemTenantSearchTooLarge | 413 | That search was too large | A search is one line of at most 254 characters. Nothing was searched. | Back to the tenants |
| problemTenantSearchRefused | 400 | That search could not be used | A search is one line of at most 254 characters, with no line breaks or control characters. Nothing was searched. | Back to the tenants |
| problemTenantPageRefused | 400 | That page does not exist | The list has pages 1 to 1000. Nothing was searched; start again from the first page. | Back to the tenants |
| problemTenantsUnreadable | 503 | The tenants could not be loaded | Try again in a moment. | Back to the console |
| problemTenantNotAnID | 404 | That link does not name a tenant | Open the tenant from the list. | Go to the tenants |
| problemNoSuchTenant | 404 | There is no tenant with that id | The id may have been copied wrongly. Open the tenant from the list. | Go to the tenants |
| problemTenantUnreadable | 503 | That tenant could not be loaded | Try again in a moment. | Back to the tenants |
| problemPlaquesUnreadable | 503 | The plaques could not be loaded | Try again in a moment. | Back to the tenants |
| problemAuditFormTooLarge | 413 | That request was too large | Open the audit log again and choose a kind there. Nothing was read. | Back to the audit log |
| problemAuditKindRefused | 400 | That kind is not in the audit log | Choose a kind from the list on the audit log. Nothing was read. | Back to the audit log |
| problemAuditPageRefused | 400 | That page does not exist | The audit log has pages 1 to 1000. Nothing was read; start again from the first page. | Back to the audit log |
| problemAuditUnreadable | 503 | The audit log could not be loaded | Try again in a moment. | Back to the console |
| problemBillingFormTooLarge | 413 | That request was too large | Open the tenant's billing again and choose a page there. Nothing was read. | Back to the tenants |
| problemBillingPageRefused | 400 | That page does not exist | A tenant's billing has pages 1 to 5. Nothing was read; start again from the first page. | Back to the tenants |
| problemBillingUnreadable | 503 | This tenant's billing could not be loaded | Try again in a moment. No figure was read, so none is shown — not even a zero. | Back to the tenants |
| problemVATUnreadable | 503 | This tenant's VAT number could not be loaded | Try again in a moment. Nothing was sent to VIES. | Back to the tenants |
| problemVATNotRecorded | 503 | The re-check was not confirmed | VIES answered, but the database did not confirm the answer was recorded. Open the tenant's VAT number again — it shows the verdict on file — and ask again from there if it has not changed. | Back to the tenants |
| problemHoldingsPageRefused | 404 | That page does not exist | The list of holdings has pages 1 to 1000. Nothing was read; start again from the first page. | Back to the holdings |
| problemHoldingNotAnID | 404 | That link does not name a holding or a business | Open the holding from the list and use its own buttons. Nothing was changed. | Go to the holdings |
| problemHoldingFormTooLarge | 413 | That form was too large | Open the holdings again and send the form from there. Nothing was changed. | Back to the holdings |
| problemHoldingsUnreadable | 503 | The holdings could not be loaded | Try again in a moment. | Back to the console |
| problemHoldingNotCreated | 503 | The holding was not confirmed | The database did not confirm it. Open the holdings before you create it again — the list shows whether it went through. | Back to the holdings |
| problemHoldingChangeNotConfirmed | 503 | The change was not confirmed | The database did not confirm it. Open the holdings again — the list shows each holding's members as the database holds them — and try again from there if nothing changed. | Back to the holdings |
| problemTenantAPIFormTooLarge | 413 | That form was too large | Open the business's overview again and send the form from there. Nothing was changed. | Back to the tenants |
| problemTenantAPINotConfirmed | 503 | The change was not confirmed | The database did not confirm it. Open the business's overview again — its API section shows the API as the database holds it — and try again from there if nothing changed. | Back to the tenants |
| problemSuspensionFormTooLarge (OP-15) | 413 | That request was too large | Open the tenant's overview again and send the form from there. Nothing was changed. | Back to the tenants |
| problemSuspensionUnconfirmed (OP-15) | 400 | The box was not ticked | A suspension needs the box under the reasons ticked: it is there so a wrong click changes nothing. Nothing was changed. Open the tenant's overview again to suspend it. | Back to the tenants |
| problemSuspensionReasonRefused (OP-15) | 400 | Choose one of the reasons | A suspension needs one of the four reasons on the overview's form. Nothing was changed. | Back to the tenants |
| problemReinstateUnconfirmed (OP-15) | 400 | The box was not ticked | Lifting a suspension needs the box above the button ticked: it is there so a wrong click changes nothing. Nothing was changed. Open the tenant's overview again to lift it. | Back to the tenants |
| problemSuspendNotConfirmed (OP-15) | 503 | The suspension was not confirmed | The database did not confirm it, which does not mean it was not made. Open the tenant's overview from the list — it shows whether the business is suspended — before you try again. | Back to the tenants |
| problemReinstateNotConfirmed (OP-15) | 503 | Lifting the suspension was not confirmed | The database did not confirm it, which does not mean it was not made. Open the tenant's overview from the list — it shows whether the business is suspended — before you try again. | Back to the tenants |

Signed in = every constant from Legal onward, plus SignOutFailed and TooMany(true). Wording
rules: problems never repeat the search term or the VAT number (`render.go:231-236, 359-364`);
an unconfirmed write never claims "nothing happened" (`render.go:381-388, 425-428`); the billing
fault never shows a figure (`render.go:335-336`). **The suspension's six (OP-15, ADR 0025 §5;
`render.go:452-502`):** the **four refusal pages** — 413 "That request was too large", the two 400
"The box was not ticked" (one per form) and the 400 "Choose one of the reasons" — are answered
before any store call and say "Nothing was changed." (the code's comment, `render.go:453`, says
"three refusals": it counts the three checks — size, box, reason — where this list counts pages);
the two 503s **never** say that —
an error from the one autocommit statement does not prove it was not committed — and send the
operator to the overview, which says what the database holds; none names the tenant or repeats a
posted value. A malformed id in either path answers `problemTenantNotAnID` (404) before any store
call (`suspension.go:163-167`, `:198-202`). Two more answers of the two routes are not suspension
pages: a body that does not parse is the shared `problemBadForm` (400, "That form could not be
read", `readForm`, `render.go:61`), and a session that died between the gate and the write is a
redirect to the sign-in (`suspensionWritten`, `suspension.go:233-234`), as the gate itself answers. Their status, headers and the no-echo rule are pinned
per response class by `TestOperatorHeaders_TheSuspensionClassesCarryThePolicy` (C218–C242); their
links by `TestProblemPages_LinkOnlyToMountedRoutes` (through `problemPages()`); that a refusal
writes nothing by `TestSuspension_NothingIsWrittenWithoutTheBoxOrAKnownReason` and
`TestSuspension_EveryRefusalWritesNothing`. The two "The box was not ticked" pages share a title
and differ in the message (suspend: "under the reasons"; lift: "above the button").

**Board states:** signed-out with back link (cross-origin) · signed-out without link (setup link
does not work) · signed-in with back (change not confirmed; nav drawn, nothing current) · 429
without link. The six suspension pages are on no board (OP-15 came later); each is a signed-in
page with a back link — the "signed-in with back" state, with the code's words.
**Not boards:** plain-text 404, 503, 500 (README §7).

---

## 5. OpTenants — `Tenants` (`tenants.templ:23-113`)

Signed-in shell, current = Tenants. Title `Tenants`.

| Element | Words |
|---|---|
| label · h1 | Tenants · Find a tenant |
| search label | Search |
| help | Part of a business name, a panel account's whole email address, or a tenant id. Leave it empty to list every tenant. |
| button | Search |
| searched line | Tenants matching `<bdi class="font-mono">term</bdi>`. + link **Show every tenant** (`<a href="/operator/tenants" class="op-link">Show every tenant</a>`, GET) |
| link | Back to the console |
| docket heading | Newest first · page `N` (N mono) |
| row | name link → overview (`<bdi>`; italic **Unnamed tenant** when the name shows nothing) · Since `<created UTC>` · plan · [chip **Suspended** — OP-15] · tenant id (xs, `text-ink/70`, break-all) |
| chip (OP-15, K15-3) | `<span class="tally tally--tenant-suspended">Suspended</span>` (`tenants.templ:81-83`), **between the plan and the id**, only on a tenant with a suspension start — `op_read_tenants` returns the start alone, no reason (`tenants.go:326-328`); a tenant that is not suspended has **no** chip on the list (the overview's "Active" chip is the overview's). Rejection tone: `border-tomato bg-tomato/10` (`input.css:696-699`; ink word 11 px on tomato/10 over paper, `TestSuspensionScreen_TheChipsAndTextClearAA`) |
| empty | page > 1: This page is empty: the list ended on the page before. · searched: No tenant matches that search. · none: There are no tenants yet. |
| pager | `<nav aria-label="Pages of the list">` **Previous page** / **Next page** |

**Forms:** search `<form method="post" action="/operator/tenants" role="search" class="…">` with `q`
(type search, maxlength 254, autocomplete off, spellcheck false, value = the term as searched).
Pager: each button a POST form to `/operator/tenants` with hidden `q` + `page`, button
`<button type="submit" class="op-link">`. Next only after a full page (50 rows,
`tenants.go:91`) and page < 1000. (Not in the polished-pager list: it keeps `op-link` unless its
board is redrawn — README §6 "Pager".)

**Layout (polished boards):** the row is a grid — desktop one line, four tracks (name · Since ·
plan · id, `OpTenants`); tablet three tracks with the id on its own full-width line under them
(`T-OpTenants`); on the phone the name link stands alone on the first line, 44 px tall
(`op-link`'s `min-h-11`) — README §6.1 "Tenants row". **The Suspended chip on that grid** (no board
draws it): the id is placed explicitly (desktop line 1, track 4), so the chip — the fifth child —
auto-places on **line 2, under the name**; a row without it keeps one line. Tablet: chip line 2,
id line 3. Phone: the chip flows after the plan. Recipe and the pins it must not break: README
§6.1 "Tenants row" (`TestTenantList_ASuspendedTenantCarriesTheChip` holds `<span>plan</span>`,
the chip and `<span class="break-all text-xs text-ink/70">id</span>` in that order, and — on a list
of one suspended and one active tenant — exactly one chip on the whole list).

**States:** every tenant page 1 · searched with matches · no match · no tenants yet · later page
empty · pager Previous + Next · a suspended row (fixture "tenants, a suspended row").
**Binding:** the term is personal data (it may be an e-mail): never in a URL, link or problem
page (`tenants.templ:12-17`).
**Not shown:** counts, plan filter, sort control, recent searches; the suspension's reason or
start on the list (the overview's), a "suspended only" filter, a suspend control on a row.
**Tests:** `TestTenantList_ASuspendedTenantCarriesTheChip`, `TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm`,
`TestTenantSearch_TheBoundaryRefusesBeforeTheStore`, `TestTenantList_PagesForwardOnlyAfterAFullPage`,
`TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped`, `TestTenantScreens_TheTextClearsAA`;
exact markup in `op11_test.go:845` (Show every tenant) and `:997` (unnamed row link).

---

## 6. OpTenant — `TenantOverview` (`tenants.templ:126-400`) in `TenantScreen`

Signed-in shell, current = Tenants; banner + sub-nav (current = Overview). Title
`Tenant overview — <name> — Taptime operator`.

| Element | Words / markup |
|---|---|
| label → **h1** | Tenant overview — today `<p class="docket-label">Tenant overview</p>` (`tenants.templ:129`), the page's only label and **no `<h1>`**; M13-08 makes it `<h1 class="docket-label">Tenant overview</h1>` (below) |
| facts `dl` | **Tenant id**, **Signed up**, **Plan**, **Business type** — pinned regex `<dt class="docket-label">LABEL</dt>\s*<dd class="mt-1 break-all font-mono text-sm">` (`op11_test.go:894`) |
| docket heading | Live now (`<section class="docket" aria-labelledby="tenant-live">`) |
| counts | **Locations**, **Active employees**, **Active plaques**, **Active panel accounts** — pinned `<dd class="mt-1 font-mono text-3xl font-bold">` (`op11_test.go:900`) |
| note | Counted when this page was opened. Locations have no status; the other three count the active ones only. |
| links | See this tenant's plaques · See this tenant's billing · See this tenant's VAT number (`<a href=… class="op-link">`, pinned in `op13_test.go:557`-area) |
| API state line | chip `tally--active` **Open** + Since `<UTC>` · or chip `tally--deactivated` **Closed**; then **No API code** or API code `<mono>KF</mono>` |
| API sentence | open: The business's integration keys work and its webhooks are sent. Every punch names it by this code. · closed with code: The business's integration keys are refused and no webhook is sent. Punches are still recorded, and keep this code. · never opened: The API has never been opened for this business. Its owner can create keys in the panel; they work once it is open. |
| API field | API code; help: A capital letter followed by 1 to 15 capital letters or digits, such as KF. No other business in its holding may have it. |
| buttons | **Open the API** (closed) / **Change the code** (open); **Close the API** (`btn--quiet`, open only) |
| suspension card (OP-15) | **Panel writes** — after the API card, before the footer links; below, "Panel writes card" |
| footer links | Back to the tenants · Back to the console |

**The overview's `<h1>` (orchestrator decision, 2026-10-25).** Measured: the overview is the one
operator data screen without an `<h1>` — Plaques, Billing and VAT each carry one, the page heading
(`plaques.templ:20` "Plaque inventory", `billing.templ:28` "Billing months", `vat.templ:25` "VAT
re-check"), and the overview's first card opens with `<p class="docket-label">Tenant overview</p>`
(`tenants.templ:129`). The `<h1>` is the **page heading, not the tenant's name**: M13-08 changes
that one element to `<h1 class="docket-label">Tenant overview</h1>` — same word, same class, only
the element (no test pins the `<p>`); it keeps the `docket-label` look, not the catalogue's display-size `<h1>` (README §6 "Card" exception). The tenant-name banner does **not** become an `<h1>`: it is
the shell's, on all four one-tenant screens, so the other three would carry two, and its pinned
`<p class="font-display text-xl font-bold tracking-tight">` would change. README §2 "OP-15 annex" (c).

**API notices** (`apiNotice` `:230-248`, words `apiSentences` `tenantapi.go:172-185`, driven by
the 303's `?done=` and decided by the fresh read):

| Tone | Heading | Body |
|---|---|---|
| ok | Opened. | The integration API is open for this business under the code below. Its keys work from the next request. |
| warn | Not opened — nothing changed. | The database opens the API under a code only when every rule below holds. The API section shows the business as it is now. + rules |
| warn | Code not changed — the API stays open under its current code. | The database changes an open API's code only when every rule below holds. Its keys keep working; the API section shows the code it has now. + rules |
| ok | Closed. | The integration API is closed for this business: its keys are refused from the next request and no webhook is sent. The code is kept. |
| warn | It is still open. | The API section shows the business as the database holds it now. Close it again from there if it should be closed. |
| warn (422) | That code cannot be used | An API code is a capital letter followed by 1 to 15 capital letters or digits, such as KF or KM. Nothing was changed. |

Rules (`tenantapi.go:189-193`): The code is a capital letter followed by 1 to 15 capital letters
or digits. · No other business in this business's holding has the code, and none that left it
less than 60 seconds ago. · The business exists.

**Forms:** `POST /operator/tenants/{id}/integration` with `api_code` (maxlength 16, mono,
required, value = current code) — exactly one; when open, `POST
/operator/tenants/{id}/integration/close` (no field) — exactly one. No confirmation step.

**States:** API open · closed with code · never opened · each notice · unnamed tenant banner ·
suspension: active, suspended, suspended with an unrecognised reason (below).
**Not shown (OP-11 minimisation, `tenants.templ:115-125`):** any person, address, plaque uid,
VAT number, holding membership, owner e-mail, keys, webhook URL. No link to Holdings or Audit
in the page body (the nav's links are the shell's).
**Tests:** `TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath`,
`TestTenantOverview_AnUnnamedTenantIsNamedByItsID`, `TestOperatorAPI_TheCodeRuleIsTheDatabases`,
`TestOperatorAPI_TheResultIsReadNotPosted`, `TestOperatorAPI_ARefusedOrFailedOpenNoticesNothing`,
`TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` (form counts), `TestTenantScreens_TheTextClearsAA`.

### Panel writes card (OP-15, ADR 0025 §5) — no board

`tenantSuspension` (`tenants.templ:254-279`): the overview's **last** card — after the Integration
API card, before the footer links (the gravest of the overview's writes at its foot, ADR 0025 C
annex). Its state is the overview's own read (`op_read_tenant_detail`, migration 00063); the card
reads nothing of its own. View `TenantSuspensionView` (`view.go:269-282`), built by `suspensionView`
(`suspension.go:124-140`). The boards predate OP-15 and draw none of it.

| Element | Words / markup (verbatim) |
|---|---|
| card | `<section class="op-card" aria-labelledby="tenant-suspension">`; `<h2 id="tenant-suspension" class="docket-label">Panel writes</h2>` (under the page's `<h1>` "Tenant overview" — above, "The overview's `<h1>`") |
| state — active | chip `<span class="tally tally--tenant-active">Active</span>` (in `<p class="mt-3">`) · The business's panel accepts its writes. |
| state — suspended | chip `<span class="tally tally--tenant-suspended">Suspended</span>` · The business's panel refuses its writes. Taps, signing in, reading every section, the CSV exports and the four paths of the time record stay open. |
| facts (suspended) | `dl` with **Suspended since** — `<dt class="docket-label">Suspended since</dt>` `<dd class="mt-1 break-all font-mono text-sm">`, UTC to the minute (`utcStamp`) — and **Reason** — `<dt class="docket-label">Reason</dt>` `<dd class="mt-1 text-sm">` the label |
| reason labels (`suspension.go:104-109`) | `non_payment` **Non-payment** · `terms_breach` **Breach of the terms** · `security_hold` **Security hold** · `customer_request` **At the business's request**. A stored code this build does not name: its raw text (mono) + `<span class="tally tally--unrecognised">Unrecognised</span>` when it has a closed-set member's shape, the chip alone otherwise (`suspensionReason`, `tenants.templ:283-291`; the audit log's `unknownWord` rule) |
| suspend form h3 (active only) | Suspend this tenant's writes |
| suspend form, first paragraph | Every write of the business's panel is refused until the suspension is lifted — except signing in and out, a panel account's own password, and the four paths of the time record: adding a person to the staff, a new invitation and both steps of a manual entry. Taps, reading every section and the CSV exports go on, and the months of a suspension are billed as usual. |
| suspend form, second paragraph | Every section shows the business a banner with a sentence for the reason: non-payment and a breach of the terms are named; a security hold and the business's own request share one neutral sentence. |
| reasons | `<fieldset>` with `<legend class="docket-label">Reason</legend>`; four radios in the closed set's order (`db.SuspensionReasons()`), each followed by its label |
| security-hold warning | warn notice, heading **A security hold freezes writes. It does not contain an account.**, body `holdWarning` (`tenants.templ:357-384`), in this order: `<p>` "Whoever holds a session of this business keeps it. While the business is suspended, that session still:" (`:358`) · `<ul class="mt-1 list-disc pl-5">` of three items (reads and CSV; staff and invitations; manual records) · `<p class="mt-2">` "The business's own defensive writes stop too: …" (`:370`) · `<p class="mt-2 font-bold">` "What does stop an account:" (`:375`) · `<ul class="mt-1 list-disc pl-5">` of three items (disabling the panel account; a password reset by the rightful admin; the rightful admin changing their own password) — three paragraphs and two lists, word for word as the code and `holdSentences` (`op15_test.go`); closed text, no tenant data. In the page whatever is chosen; the stylesheet shows it only while **Security hold** is checked |
| box | I have checked that this is the right tenant. |
| suspend button | **Suspend writes** (`btn btn--quiet self-start`) |
| reinstate form h3 (suspended only) | Lift the suspension |
| reinstate paragraph | The business's panel accepts its writes again from its next request. The suspension and its lifting stay on record in the business's own audit log and in the operator audit log. |
| reinstate box | I have checked that this is the right tenant. |
| reinstate button | **Lift the suspension** (`btn btn--primary self-start`) |

**Forms — exactly one, by state** (`op8_test.go:921-926`):
- suspend (active): `<form method="post" action="/operator/tenants/{id}/suspend" class="mt-5 flex flex-col gap-3">`
  — fields **only** `reason` (radio, `required`) and `confirm` (checkbox, `value="yes"`, `required`);
  no tenant field, no text field, no `<textarea>` (`TestTenantOverview_SaysTheSuspensionAndOffersOneForm`).
- reinstate (suspended): `<form method="post" action="/operator/tenants/{id}/reinstate" class="mt-5 flex flex-col gap-3">`
  — field **only** `confirm`.
- No CSRF field and none is added: the forgery defence is `sameOriginGate(true)` + the session
  cookie's SameSite=Strict (`suspension.go:37-39`); the box is against a wrong click. The server
  re-checks everything (box exactly `yes`, reason a closed-set code, the id's 36-character path
  form) before any store call — §4. Success: 303 back to the overview, **no success notice** (the
  fresh read shows the state); there is no "already suspended" / "nothing changed" message (ADR 0021
  §2 v 7, `suspension.go:24-28`).

**Pinned markup — keep byte for byte** (`op15_test.go`):
- radio `<input id="suspend-reason-CODE" class="…" type="radio" name="reason" value="CODE" required`
  (attribute order id, class, type, name, value, required); the security-hold radio's class
  **starts with** `op-reason-hold ` and it continues `value="security_hold" required
  aria-describedby="suspend-hold-warning"`; exactly one radio carries `op-reason-hold`;
- label `<label for="suspend-reason-CODE" class="inline-flex min-h-11 items-center text-sm">LABEL</label>`;
- box `<input type="checkbox" name="confirm" value="yes" required class="h-5 w-5 shrink-0 accent-tappa-green">`;
- the radios' parent `<div class="op-reasons mt-1">` and, **after all four radios in that parent**,
  `<div id="suspend-hold-warning" class="op-hold-warning">`; the test cuts the warning at its first
  `</section>` — `opNotice`'s root since M13-03 (it was `components.Notice`'s); M13-03 kept the `<section>`
  root, so the test's cut is unchanged and still holds the heading and every sentence;
- `input.css` holds **exactly once** each of `.op-reasons { @apply grid grid-cols-[auto_1fr] items-center gap-x-3; }`,
  `.op-hold-warning { @apply col-span-2 mt-2 hidden; }` and
  `.op-reason-hold:checked ~ .op-hold-warning { @apply block; }` (`input.css:1711-1713`); when
  `app.css` is built, it carries `.op-reason-hold:checked~.op-hold-warning{display:block}` and
  `.op-hold-warning{display:none;…}` — a port that restyles these changes the test in the same commit;
- the chips (`chipActive`, `chipSuspended`), the two facts' `dt`/`dd` regexes, and the chip rules —
  `tally--tenant-active` exactly `border-tappa-green bg-green-lite`, `tally--tenant-suspended`
  exactly `border-tomato bg-tomato/10` (`TestSuspensionScreen_EachChipHasTheRuleTheContrastTestComputes`).

**Recipe — built from the catalogue (README §6), same words, same order, same forms (D1):**
- An `op-card` like the API card above it (M13-02: `rounded-[14px]`, no shadow, the single padding
  scale). The h2 stays `docket-label` "Panel writes", as its sibling "Integration API"; the two
  `<h3>`s keep their element and size (`font-display text-lg font-bold tracking-tight`).
- The state chip alone on its line, rounded by `.op-main .tally` (M13-02) — no class on the span.
- Facts: the `dl`'s `sm:grid-cols-2` becomes `md:grid-cols-2` (this surface's breakpoints are base /
  `md:` / `desk:`; two facts, so no `desk:grid-cols-4`); the `dt`/`dd` stay as pinned.
- Reasons: the `.op-reasons` two-column grid (radio · label) as coded; the label is the 44 px target.
- Warning: `opNotice(warn, heading)` (M13-03) inside the `op-hold-warning` div, its lists
  `list-disc pl-5` as coded. Shown by the general-sibling rule alone — **no `:has()`, no script**
  (the overview's CSP carries no `script-src`); a browser without the stylesheet shows it always.
  Ink/85 on saffron-lite is the warn notice's measured pair (README §6 "Operator notice").
- Buttons: `btn` with the catalogue's `rounded-[10px]`; **Suspend writes** quiet (tomato on hover),
  **Lift the suspension** primary, each `self-start` as coded.

**States:** active (fixture "tenant overview") · suspended, a known reason ("tenant overview,
suspended") · suspended, an unrecognised reason ("tenant overview, an unrecognised reason") ·
Security hold checked (the warning shown — a CSS state; the suite measures the markup and the rules,
not a browser) · unnamed tenant (the card is the same).
**Not shown:** a free-text reason; a "change the reason" form for a suspended tenant (ADR 0025 C
annex LC2 — not decided, a v2 candidate); a confirm step or modal; a platform-wide list of
suspensions (`/operator/suspensions`, `…/{id}/suspension` stay unmounted —
`TestSurface_TheScreensOfLaterTasksAreNotMounted`, `op8_test.go:1139`); the tenant's banner sentence;
the tenant's own `audit_log` rows; who suspended it (the audit log's row, §10).
**Tests:** `TestTenantOverview_SaysTheSuspensionAndOffersOneForm`,
`TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice`, `TestSuspensionWords_NameEveryReasonAndNothingElse`,
`TestSuspensionScreen_TheChipsAndTextClearAA`, `TestSuspensionScreen_EachChipHasTheRuleTheContrastTestComputes`,
`TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` (one form by state),
`TestOperatorHeaders_TheSuspensionClassesCarryThePolicy`, `TestSuspension_TheTenantIsThePathsNotTheForms`,
`TestSuspension_TheAnswerIsTheSameWhateverTheTenantHeld`, `TestSuspension_NoPostedValueReachesALogLine`,
`TestSuspensionBudget_AWriteIsOneRequestAndNoRead`, `TestSuspension_TheListedSitesAloneWrite`;
against PostgreSQL `TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant` and
`TestE2E_TheOperatorsSuspensionClosesThePanelAndTheAPIAndItsLiftingOpensThem`.

---

## 7. OpPlaques — `TenantPlaques` (`plaques.templ:16-101`) in `TenantScreen`

Banner + sub-nav (current = Plaques). Title `Plaques — <name> — Taptime operator`.

| Element | Words |
|---|---|
| label · h1 | Plaques · Plaque inventory |
| summary (one of) | This tenant has no plaques. · The first `<200>` of `<N>` plaques are listed, in the panel's own order: those with no location first. · `<N>` plaque(s), in the panel's own order: those with no location first. |
| note | Read when this page was opened. Nothing on this page changes a plaque. |
| links | Back to the overview · Back to the tenants |
| docket heading | Plaques of this tenant (only when rows exist) |
| row `op-row` | UID (mono bold) · chip · sentence (full width) · [Stored status "…"] · No location / At `<bdi>Name</bdi>` / At an unnamed location `<id>` · Encoded `<UTC>` / No encode recorded · Added `<UTC>` · [Retired `<UTC>`] · [Replaced by `<uid>`] · Counter `<n>` |

Chips (`plaques.go:136-149`):

| Word | Tone | Sentence |
|---|---|---|
| On a wall | `tally--mounted` | In service at its location; its encode was recorded. |
| On a wall, never encoded | `tally--unencoded` | Mounted and in service, but no encode was ever recorded for it. |
| In stock | `tally--stock` | Not on a wall; encoded and ready to mount. |
| In stock, not encoded | `tally--unencoded` | Not on a wall, and it cannot be mounted until its encode is recorded. |
| Retired | `tally--withdrawn` | Taken out of service, with or without a replacement; taps on it are rejected. |
| Lost | `tally--withdrawn` | Reported lost; taps on it are rejected. |
| Unrecognised | `tally--unrecognised` | Its stored status and location are not a state this screen knows; the status is shown as stored. |

**Layout (polished boards):** two columns on tablet/desktop — the UID in its own column, chip,
sentence and facts beside it (one wrapper `<div>` after the UID span; the row still starts with
the UID span, `op13_test.go:285`); stacked on the phone — README §6.1 "Plaque row".

**Forms:** none — the page's only `<form` is the band's sign-out and its only `<button` the
sign-out's text button; no `hx-` attribute (`plaques.templ:7-9`, `op13_test.go:327-332`). The nav
and sub-nav are links, and the phone sign-out stays text (no icon).
**States:** mixed rows covering all seven chips · truncated "first 200 of N" · no plaques ·
unnamed tenant (drawn on the polished `OpPlaques` boards too; fixture "tenant plaques, unnamed",
`op8_test.go:747`). A **suspended** tenant's plaques render the same: this screen reads and shows
no suspension (OP-15 changed neither `plaques.go` nor `plaques.templ`).
**Not shown:** AES key, key ref, CMAC, tap URL, a QR of the plaque (D4's plaque drawing is a
panel feature).
**Tests:** `TestPlaqueScreen_NoKeyReachesThePage`, `TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible`,
`TestPlaqueWords_NameEveryShapeAndNothingElse`, `TestPlaqueScreen_NamesTheTenantAndRefusesABadPath`,
`TestPlaqueScreen_TheFirst200OfNAndAnEmptyTenant`, `TestPlaqueScreen_TheChipsAndTextClearAA`,
`TestPlaqueScreen_EachChipHasTheRuleTheContrastTestComputes`.

---

## 8. OpBilling — `TenantBilling` (`billing.templ:24-134`) in `TenantScreen`

Banner + sub-nav (current = Billing). Title `Billing — <name> — Taptime operator`.

| Element | Words |
|---|---|
| label · h1 | Billing · Billing months |
| intro | `<12>` of this tenant's own months per page, newest first, each in the time zone it is counted in. These are the tenant's own figures: the arithmetic of its billing screen, read when this page was opened. |
| note | Nothing on this page closes, changes or charges a month. A count is all a month keeps: no person is listed, here or anywhere a month links. |
| links | Back to the overview · Back to the tenants (exact markup `op12_test.go:1445-1446`) |
| docket heading | Newest first · page `N` of 5 (pinned, `op12_test.go:~1433`) |
| row `op-row` | month ("October 2026", mono bold) · chip · sentence · with figures: Plan `<x>` · People counted `<n>` · Price per person `<€x.xx>` · Amount `<€x.xx>` · [Closed `<local time zone>`] · [First charged month `<Month YYYY>`] · Counted over `<from>` to `<to zone>`; [free-window sentence]; [floor sentence] |
| figures (polished boards) | the four figures are drawn as a **receipt strip**, small labels above values — PLAN · PEOPLE COUNTED · PRICE PER PERSON · AMOUNT (the code's words; the caps are CSS), Amount right-aligned, 20 px bold; 2 × 2 on the phone — instead of today's inline `<span class="text-sm">Plan <span class="font-mono">…</span></span>` spans (`billing.templ:68-71`). Markup and test changes: README §6 "Billing receipt strip", M13-10. Closed / First charged month / Counted over stay inline spans. |
| free window | Inside the founding offer's free window: the amount is zero because of the offer, not because nobody was counted. |
| floor | The count is a FLOOR: `<n>` employee records of this business disagree with their own dates and are never counted, so the real count may be higher. That number is an upper bound on the effect, not the amount missing. |
| last page | This is the oldest page this screen shows: `<60>` months, the tenant's own history depth. |
| pager | `aria-label="Pages of the billing months"`: **Newer months** / **Older months** |

States (`billing.go:235-249`):

| Word | Tone | Sentence | Figures |
|---|---|---|---|
| Frozen | `tally--frozen` | Closed by the business; every figure was read from the frozen record, and nothing recomputes it. | yes |
| Live — month running | `tally--running` | A live count, worked out afresh on each view: it moves until the month ends and the business closes it. | yes |
| Ended, not closed by the business | `tally--unclosed` | This month is over and the business has not closed it, so its figures are still a live count, worked out afresh on each view. | yes |
| Before sign-up | `tally--before-signup` | This month is before the business signed up; there is nothing to count for it. | **none** |
| Unreadable | `tally--unreadable` | This month's row could not be read as a billing month, so no figure is shown for it — not even a zero. | **none** (label may be "A month with no date") |

Chip tones as the polished boards draw them: live (`tally--running`) = **dashed saffron
outline on paper** (a re-tone of its own rule, README §6 "Chip / tally"); ended, not closed = saffron;
frozen = green; before sign-up = line; unreadable = ink (the last four as today).

**Forms:** pager — POST forms to `/operator/tenants/{id}/billing` with hidden `page` (1..5);
exactly 2 on page 3 (`op8_test.go:895`). Drawn as outlined buttons: `<button type="submit"
class="op-pager">` after M13-10 (today `class="op-link"`, pinned at `op12_test.go:1372, 1441`).

**States:** every tone on one page · page 3 (both pager buttons) · the last page (oldest-page
note) · unnamed tenant (fixtures "tenant billing", "tenant billing, page 3", "tenant billing, the
last", "tenant billing, unnamed", `op8_test.go:753-756`). A **suspended** tenant's months render the
same — a suspension's months are billed as usual (ADR 0025 §6) and this screen shows no
suspension.
**Binding:** no word claims owed / late / paid (`billing.go:230-233`); a frozen and a live month
never render alike; unreadable / before-sign-up rows show no figure (§4.6).
**Not shown:** any person, invoice PDF, payment status, CSV, close / freeze control.
**Tests:** `TestBillingScreen_EveryStateSaysItsWordAndNoVerdict`, `TestBilling_AFrozenMonthAndALiveMonthDoNotRenderAlike`,
`TestBillingScreen_SaysTheCountIsAFloor`, `TestBillingScreen_OffersNoItemisationAnywhere`,
`TestBillingScreen_AFailedReadIsAProblemPageNeverAZeroInvoice`, `TestBillingScreen_ReadsNothingFromTheURL`,
`TestBillingScreen_TimesAreTheMonthsOwnZone`, `TestBillingScreen_PagesAreFiveOfTwelve`,
`TestBillingScreen_WearsTheDocketAnatomy`, `TestBillingScreen_TheChipsAndTextClearAA`,
`TestBillingScreen_EachChipHasTheRuleTheContrastTestComputes`, `TestBillingMoney_PutsTheSymbolInTheRenderLayer`.

---

## 9. OpVat — `TenantVAT` (`vat.templ:21-96`) in `TenantScreen`

Banner + sub-nav (current = VAT number). Title `VAT number — <name> — Taptime operator`.

| Element | Words |
|---|---|
| label · h1 | VAT number · VAT re-check |
| intro | The VAT number this business registered with, and the answer of the EU VAT register (VIES) on file for it. |
| links | Back to the overview · Back to the tenants |
| docket heading | On file |
| facts | **VAT number** (`<dd class="mt-1 break-all font-mono text-lg font-bold">`, as stored) · **VIES answer** chip + sentence |
| time line | Last answer `<UTC>` (answered) · Asked `<UTC>` (no verdict) · No time of asking is on file. |
| card | Ask again |
| askable | Sends the number above to VIES now. If VIES answers, its answer replaces the one on file and is written to this tenant's own audit log; if it does not answer, nothing changes. + button **Ask VIES again** |
| not askable | This number is not in the format VIES accepts for its country, so it cannot be sent from here. Only the platform owner can correct it, in the database. (no form) |

Verdicts (`vat.go:363-375`; words match the tenant Account screen):

| Word | Tone | Sentence |
|---|---|---|
| Not checked | `tally--vat-not-checked` | No answer from VIES is on file: nobody asked, or the sign-up asked and could not reach the register — the record cannot tell them apart. |
| No answer | `tally--vat-no-answer` | VIES was asked about this number and did not answer — an outage at their end, not a verdict about the business. |
| Confirmed | `tally--vat-confirmed` | VIES confirmed this number when it last answered. |
| Not found | `tally--vat-not-found` | VIES did not recognise this number when it last answered. Nothing in the product depends on it; an invoice to a number the register does not know is a matter for the business's accountant. |

Warn notices (a POST that changed nothing, `vat.go:394-401`):

| Status | Heading | Body |
|---|---|---|
| 503 | VIES did not answer | The EU VAT register (VIES) did not answer. Nothing was changed; the result below still stands. Try again in a few minutes. |
| 422 | This number cannot be sent to VIES | It is not in the format VIES accepts for its country, so it was not sent. Nothing was changed. The number can only be corrected by the platform owner, in the database: neither the business nor this screen can change it. |
| 429 | Too many VAT re-checks | This session has asked VIES ten times in ten minutes. Wait ten minutes, then try again. Nothing was changed. |

**Layout (polished boards):** the two "On file" facts side by side on tablet/desktop (the `dl`,
`vat.templ:41`), stacked on the phone; the pinned `<dd class="mt-1 break-all font-mono text-lg
font-bold">` stays (`op16_test.go:1409`).

**States:** each of the four verdicts · never asked · a warn notice · odd number (no form) ·
unnamed tenant (drawn on the polished `OpVat` boards too; the fixture "tenant VAT, not found"
carries the unnamed banner, `op8_test.go:684, 760`). A **suspended** tenant's VAT screen renders the
same: it reads and shows no suspension.

**Form:** `POST /operator/tenants/{id}/vat` with **no field** — only the button; the server sends
the number it read (`vat.templ:10-14`). Exactly one when askable, none otherwise.
**Success:** 303 back, no success notice (the fresh read shows the verdict).
**Not shown:** an edit field for the number (the definer holds no UPDATE on `vat_number`); the
VAT number anywhere but this screen.
**Tests:** `TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem`,
`TestVATScreen_TheFourStatesAreTheAccountScreensFour`, `TestVATScreen_EveryStateSaysItsWordAndSentence`,
`TestVATScreen_TheChipsAndTextClearAA`, `TestVATScreen_EachChipHasTheRuleTheContrastTestComputes`,
`TestVATRecheck_TheNumberIsTheServersNotTheClients`, `TestVATRecheck_TheNumberIsOnNoLogLineOrHeader`,
`TestVATNumber_TheListedSitesAloneReadIt`.

---

## 10. OpAudit — `AuditLog` (`audit.templ:27-201`)

Signed-in shell, current = Audit log; no banner. Title `Audit log`.

| Element | Words |
|---|---|
| label · h1 | Audit log · Who did what on this surface |
| intro | Every sign-in, sign-out, read and publication on the operator surface, newest first, for every operator. Opening this page is a read too, and it is listed. |
| filter | label **Kind**; `<select id="audit-kind" name="kind" class="op-input">` options **Every kind** + one per kind; button **Show** |
| link | Back to the console |
| docket heading | Newest first · `<Filter word>` · page `N` — pinned `<h2 id="audit-list" class="docket-label">Newest first · Signed in · page <span class="font-mono">2</span></h2>` |
| empty | page > 1: This page is empty: the log ended on the page before. · The log holds no entry of this kind. |
| last page | This is the last page the screen shows: the newest `<50,000>` entries of the kind chosen. |
| pager | `aria-label="Pages of the audit log"` Previous page / Next page |

Kind words (`audit.go:191-228`; 22 kinds — the derived count `TestAuditWords_NameEveryKindAndNothingElse`
checks): Sign-in refused: wrong password · Sign-in refused: no active
account with that address · Code refused · Account locked after wrong codes · Setup link refused
· Password accepted, before the code · Signed in · Set up and signed in · Signed out · Read ·
Legal text published · Operator account created · Operator account reset to a new setup link ·
Operator account disabled · VAT re-check recorded · Integration API opened · Integration API
closed · Holding created · Asked to add a business to a holding · Asked to remove a business
from a holding · **Tenant suspension recorded** · **Tenant reinstatement recorded** (OP-15,
`audit.go:226-227`; kinds `tenant_suspended`, `tenant_reinstated`, `internal/db/operator.go:1630-1631`).

**The two OP-15 kinds** are written on every accepted call, whether or not the tenant exists and
whatever it held, so the word says the operator's action was **recorded**, not that a state moved
(`audit.go:223-225`). Their row is the tenant-targeted shape the VAT and API rows have: time · word ·
actor · **Tenant** fact (link / unnamed / an id no tenant has — `audit.templ:103-114`, as for
every row with a tenant); the row's `detail` is `{}` (migration 00063), so the **reason is not on this
screen** — it is in the tenant's own `audit_log` (`tenant.suspended`), which this surface does not
read. The filter's `<select>` lists them as two more options (Every kind + 22), in the closed set's
order (`internal/db/operator.go:893-903`). Measured end to end: the filtered log shows "Tenant
suspension recorded" with the overview link and the tenant's name
(`TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant`, `op15_db_test.go:205-208`).

Row `op-row`, one fact per span: time (UTC to the second, mono bold) · kind word (display bold)
or raw mono + chip **Unrecognised** · actor: By the platform owner, with opadmin / Before sign-in
/ By `<bdi>name</bdi>` / By an unnamed operator `<id>` · [Account `<name>` / Account of an
unnamed operator `<id>`] · [Tenant `<link>name</link>` / Tenant `<link><i>Unnamed tenant</i></link>`
`<id>` / A tenant id no tenant has `<id>` (no link)] · [Of the legal texts / the tenant list / a
tenant's overview / a tenant's plaques / this audit log / a tenant's billing / a tenant's VAT
number / the holdings (`audit.go:239-248`) / Of a scope this screen does not show] · [Page n (m
per page)] · [No search term / Searched by name / Searched by address / Searched by tenant id —
**the class, never the term**] · [Filter: `<word>`] · [Document `<mono slug>`] · [`<n>` bytes] ·
[Detail not shown, italic] · [Session `<first 8 hex>`].

Faces (polished boards = the code): the operator and account **names** are Space Grotesk — the
code writes them in `<bdi>` with no mono (`audit.templ:92, 98`); mono only where the code prints
it: the time, page numbers, session, ids, document slug, bytes. The "Of a scope this screen does
not show" fact (`audit.templ:117-118`, a scope newer than this screen's words) is a row of its
own on the boards and in `TestAuditScreen_EveryRowSaysWhatTheLogHolds` (`op14_test.go:335`). The
inline tenant link keeps its line height on tablet/desktop and gets a **44 px invisible overlay**
(README §6 "Inline link hit area"); its markup stays as pinned (`op14_test.go:317, 328, 387`).

**Forms:** filter `POST /operator/audit` (`kind`); pager POST forms with hidden `kind` + `page`,
buttons `<button type="submit" class="op-link">` today, drawn as outlined buttons →
`class="op-pager"` in M13-12 (the pin `op14_test.go:742-743` changes with it); 3 forms when
filtered (`op8_test.go:929`). 50 rows per page, 1000 pages max.

**States:** every row shape (fixture "audit log") · filtered · past the end ("This page is
empty: the log ended on the page before.") · the last page · empty ("The log holds no entry of
this kind.") — `op8_test.go:748-752`.
**Not shown:** export, per-tenant audit view, derived verdicts ("compromised"), search terms,
credentials.
**Tests:** `TestAuditScreen_WearsTheDocketAnatomy`, `TestAuditScreen_EveryRowSaysWhatTheLogHolds`,
`TestAuditWords_NameEveryKindAndNothingElse`, `TestAuditWords_NameEveryScopeTheNewestMigrationReturns`,
`TestAuditScreen_ReadsNothingFromTheURL`, `TestAuditScreen_PagesForwardOnlyAfterAFullPage`,
`TestAuditScreen_EscapesWhatOperatorsAndTenantsNamed`, `TestAuditScreen_NoCredentialFieldReachesThePage`,
`TestAuditScreen_TheTextClearsAA`, `TestAuditScreen_TheOwnersRowsNameTheOwner`; the OP-15 rows:
`TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant` (PostgreSQL).

---

## 11. OpHoldings — `Holdings` (`holdings.templ:24-176`)

Signed-in shell, current = Holdings; no banner. Title `Holdings`.

| Element | Words |
|---|---|
| label · h1 | Holdings · Holdings |
| intro | A holding groups businesses that work in one timezone; while a business is in one, and for 60 seconds after it leaves, its owner cannot change its timezone. Only this screen changes who is in a holding. |
| link | Back to the console |
| create card | **Create a holding** (today `<h2 id="holding-create" class="docket-label">`, `holdings.templ:40`; drawn as a **19 px display heading** → `.op-heading`, id kept — no test pins the class); label **Name**; help: One line of at most 120 characters. The new holding has no business in it yet.; button **Create** |
| docket heading | Newest first · page `N` |
| empty | This page is empty: the list ended on the page before. · There are no holdings yet. |
| holding entry | name (display lg; italic **Unnamed holding**) · Created `<UTC>` · id (xs) · [Its first memberships are on the page before.] · members or "No business has been in this holding." · [More of its memberships are on the next page.] · add form |
| add form | label **Add a business**; help: The business's id, from its overview. It must work in the timezone of every member.; button **Add** |
| member row `op-row` | name link → overview (italic Unnamed tenant) · chip `<span class="tally tally--active">Live</span>` / `<span class="tally tally--deactivated">Ended</span>` · Timezone `<mono>` · No API code / API code `<mono>` · Joined `<UTC>` · [Left `<UTC>`] · tenant id (xs) · live only: **Remove** (`btn--quiet`). The polished boards draw the tenant id **under the name**: visual order only (CSS `order`, README §6.1 "Holdings"); the DOM keeps the code's order (`holdings.templ:131-160`) — no test pins it. |
| pager | **links** `?page=N` Previous page / Next page (`holdings.templ:165-176`), drawn as outlined buttons → `<a … class="op-pager">` in M13-13 (no test pins the link's class; the `href` pins stay, `op8_test.go:917-918`, `holdings_test.go:741, 757`) |

Notices (`holdingNotice` `:70-88`, words `holdings.go:443-460`; from the 303's `?done=` checked
against the page's own read):

| Tone | Heading | Body |
|---|---|---|
| ok | Created. | The new holding is in the list below. |
| ok | Added. | The business is a live member of this holding. |
| warn | Not added — nothing changed. | The database adds a business only when every rule below holds. The list shows each member's timezone and API code. + rules |
| ok | Removed. | The business can join a holding again, and its owner can change its timezone, after 60 seconds. |
| warn | Nothing was removed — it was not a live member. | The list below shows the holding as the database holds it now. |
| warn | It is a live member of this holding. | The list below shows its membership as the database holds it now. Remove it from there if it should leave. |
| warn | The result is not on this page. | This page does not hold the whole holding, so it cannot say what happened. Find the holding below or on the next pages. |
| warn (422) | That name cannot be used | A holding's name is one line of 1 to 120 characters, with at least one visible character. Nothing was created. |
| warn (422) | That is not a business id | Paste the business's id from its overview: 36 characters of letters, digits and hyphens. Nothing was changed. |

Rules (`holdings.go:465-470`): Its timezone is the timezone of every member of the holding. · It
is in no other holding, and at least 60 seconds have passed since it last left one. · Its API
code is not one a member of the holding already has. · The holding and the business both exist.

**Forms:** create `POST /operator/holdings` (`name`, maxlength 120, required, autocomplete off) —
one; add `POST /operator/holdings/{id}/members` (`tenant_id`, maxlength 64) — one per holding;
remove `POST /operator/holdings/{id}/members/{tenant}/remove` (empty body) — one per live
membership. No confirmation step. A refused name is **not echoed back**. 200 memberships per
page (`db.HoldingsPerPage`, `internal/db/operator.go:1509`); the page number is not personal
data (`holdings.templ:163-164`).
**States:** holdings with live and ended members (fixture "holdings") · empty ("There are no
holdings yet.") · **page 3, past the end** — "This page is empty: the list ended on the page
before." (`holdings.templ:51-52`; fixture "holdings, past the end", `HoldingsView{Page: 3}`,
`op8_test.go:765`; its Previous link still points to page 2, `op8_test.go:918`) · added ·
not added (+ rules) · unnamed holding and member split across pages ("holdings, unnamed").
**Not shown:** rename / delete holding, a tenant picker or search (the id is pasted), a
per-holding page.
**Tests:** `TestHoldings_CreateAddRemoveThroughTheScreen`, `TestHoldings_TheResultIsReadNotPosted`,
`TestHoldings_PagesForwardOnlyAfterAFullPage`, `TestHoldings_TheNameIsCheckedAtTheBoundary`,
`TestHoldings_IDsAreCheckedAtTheBoundary`, `TestHoldingsScreen_EscapesWhatOperatorsAndBusinessesNamed`,
`TestHoldingsBudget_AViewIsOneReadAndAWriteIsNone`; chip markup `holdings_test.go:~689, ~987`;
pager links `holdings_test.go:741-757`.

---

## 12. OpLegal — `Legal` (`legal.templ:21-107`)

Signed-in shell, current = Legal texts; no banner. Title `Legal texts`.

| Element | Words |
|---|---|
| label · h1 | Legal texts · Taptime's legal texts |
| intro | Publishing adds a new version; nothing is overwritten. To undo a change, publish the earlier text again. |
| public pages | The public pages are on the customer site, under /legal. — **text, not a link** (`view.go:163-165`) |
| link | Back to the console |
| editor card (privacy, terms, imprint, cookies — `internal/domain/legal/legal.go:67`) | h2 = path mono ("/legal/privacy") · Live since `<UTC>` / Not published. The public page shows its placeholder. |
| warn notice | **The public page is behind** — The newest version, published `<UTC>`, has not reached the public page yet. This editor opened on the text the page shows, so publishing it replaces the newest version. + Reload this screen to try again. The page also catches up after the next publication or a restart. |
| field | label **Text**; `<textarea name="body" rows="12" required>` prefilled with the live text; help (`legal.templ:70-74`, since the legal-links change, ADR 0033): A blank line starts a new paragraph. Addresses starting with http:// or https://, e-mail addresses and phone numbers starting with + become links; `<span class="font-mono">[text](https://…)</span>` links your own text. At most 256 KiB. — the boards draw the earlier two-sentence help; the code's wins (D1) |
| button | Publish a new version |
| docket heading | Versions, newest first |
| version row | `<li class="op-version">` published UTC · path · `<n>` bytes · publisher (operator name / operator / **tenant admin (legacy)**) · chip `tally--active` **Live** on each document's current version |
| empty | Nothing has been published yet. |
| cap | The newest 100 are listed. Older versions are not shown here. |

**Layout (polished boards; README §6.1 "Legal editor", "Legal versions"):** one **full-width
editor card per document**, side inset 24 / 24 / 20 px desktop / tablet / phone (the single card
scale) — header row with the path mono on the left and a decorative status
dot + "Live since …" / "Not published. …" on the right; the "public page is behind" notice
between that header and the form (the code's order, `legal.templ:54-65`); a **framed textarea**
with a "TEXT" label bar (the `Text` label, caps by CSS), mono 14 px / line-height 1.7,
`rows="12"`; help on the left and **Publish a new version** on the right (full-width button on
the phone). Version list: a **five-column grid** on tablet/desktop (time bold mono · path ·
bytes right-aligned · publisher · Live chip), **stacked cards** on the phone; `<li
class="op-version">` stays literal.

**Form:** one per document, `POST /operator/legal` with hidden `slug` + `body`.
**Success:** 303 back, no success notice.
**Not shown:** old versions' text, "republish this version", preview, diff (each would need a new
`op_read_*`, `legal.templ:15-17`); a link to the customer host.
**Tests:** `TestLegalPage_ShowsWhoPublishedEachVersion`, `TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`
(with `op10_test.go`'s `warnNotice` regex — updated in M13-03 to `opNotice`'s `<section role="status" class="op-notice op-notice--warn">`),
`TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind`,
`TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt`, `TestLegalScreen_TheTextClearsAA`,
`TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget`.
