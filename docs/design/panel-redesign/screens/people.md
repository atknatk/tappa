# People, person form, invite result, manual entry

Boards: `People` · `T-People` · `M-People`, `StaffForm` · `T-StaffForm` · `M-StaffForm`, `Invite` ·
`T-Invite` · `M-Invite`, `Manual` · `T-Manual` · `M-Manual`. Citations refer to **HEAD `b284899`**.
Decisions and constraints: [README](../README.md). Status words: [pass-ledger](pass-ledger.md).

## 0. Cross-cutting

| # | Fact | Evidence | Port |
|---|---|---|---|
| X1 | The shell owns the `<h1>` | `admin.templ:428-430`, `adminview.go:393-413` | kickers, breadcrumbs ("People / Ahmed Hassan / Invitation") and titles ("New person", "Invitation", "Manual entry") use the M12-01 slots; no second `<h1>` |
| X2 | Manual entry is filed under The Pass | `manualentry.go:691` (`a.chrome(r, pages.TabTransactions)`) | **KEPT AS CODE (D1)** — boards are corrected to show The Pass as current on Manual |
| X3 | No script on these pages | `employees.templ:8-16`, `manualentry.templ:11-17`, `TestEmployeesSection_LoadsNoScript` (`employees_test.go:758`) | chips, rows, segmented In/Out are links / radios / posts; Copy and live preview **REMOVED** |
| X4 | Lifecycle vocabulary is the database's | `components/roster.templ:393-397`; `handler/employees.go:92-101`; `employees_db_test.go:402-407` | **KEPT AS CODE (D1):** INVITED / ACTIVE / DEACTIVATED on chip, filter, pill and tests. "Stopped" and "Invited · waiting for first tap" **REMOVED**. |
| X5 | Deactivating does **not** sign phones out (user decision 2026-08-07) | `internal/domain/tenant/staff.go:788-798`; `employees.templ:55-64` | the card prints the real live-session count for a deactivated person; "Unlinked when stopped" **REMOVED** |
| X6 | New SQL reading `transactions.verdict` must be classified | `internal/db/evidencereaders_test.go:76` (`evidenceColumnRE`), `:44` | last tap and last 7 taps get entries; never select `note`, `trust`, `ip_match`, `gps_*`, `matched_sid` |
| X7 | The touch-target test scans every press target | `dashboard_test.go:915`; `TestEmployeesSection_ConditionalControlsCarryATouchTargetClass` (`employees_test.go:701`) | register `.roster-row` (`min-h-[60px]`) and `.chip-link` |
| X8 | Row tests find rows with a regex on `<article class="docket">` | `employees_db_test.go:203` (`rosterRowRE`), byte-ceiling test `:865` (ceiling at `:857`) | change the regex to the new stable marker (e.g. `<a class="roster-row`) **in the same commit**; keep per-person isolation and the ceiling |
| X9 | Brand: roster row vs docket | `roster.templ:3-36` | list rows are approved (the user's design); rewrite that comment; the docket stays on records and the mini dockets; chips `rounded-full` (D2) |
| X10 | Copy that states a product rule binds | per screen below | |

---

## 1. People — `/admin/employees`

### 1.1 Wiring

| Item | Where |
|---|---|
| Route | `GET /admin/employees` (`internal/handler/dashboard.go:66`) |
| Handler | `employeesSection` (`internal/handler/employees.go:155`) → `employeesView` (`:175`) → `rosterAdd` (`:283`), `rosterActions` (`:430`), `confirmedRosterAction` (`:588`); `rosterRowView` (`:667`), `deviceLine` (`:706`); `rosterFilterFrom` (`:774`), `rosterQuery` (`:817`) |
| Domain | `ledger.Reader.Roster` (`internal/domain/ledger/roster.go:249`, `RosterPageSize` `:92`); `tenant.Staff.Person` (`staff.go:316`) |
| SQL | `ListPanelEmployees` (`db/queries/employees.sql:229`; no role, no last tap; `ORDER BY full_name, id`); `GetRosterCursorAnchor` (`:423`); `GetPanelEmployeeForAction` (`:468`, also the read inside `Deactivate` / `Move` — **do not widen**) |
| Templates | `pages/employees.templ` `AdminEmployees` (`:49`); `components/roster.templ` (`RosterRow` :37, `RosterActions` :120, `lifecycleTally` :398, `RosterFilterBar` :426); `components/staffform.templ` `RosterAdd` (`:28`) |
| View models | `EmployeesView` (`employeesview.go:16`); `RosterRowView` (`rosterview.go:40`), `RosterActionsView` (`:109`), `RosterFilterView` (`:220`); `RosterAddView` (`staffformview.go:18`) |
| Selection | `?manage=<id>` (rows carry `ManageHref`, `rosterview.go:80`); deactivation confirm `?manage=<id>&confirm=deactivate` (`employees.go:489`, `:649`) |

### 1.2 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Kicker "Who taps · 112 on the books" | total of the three statuses | **BUILT (D3, M12-17)** | failed count → kicker without a number, never 0 |
| Title "People" | shell | EXISTS | |
| Search box | `RosterFilterBar` name input (`roster.templ:429-439`) → `full_name ILIKE` | KEPT AS CODE | placeholder "Part of a name" (it matches names only); "role / venue" search **REMOVED**; keep `role="search" aria-label="Filter employees"` (`TestEmployeesSection_IsTheOnlySectionRenderingTheRoster` marker); one submit for the bar |
| Add button | `RosterAdd` closed state "Add somebody" (`staffform.templ:54-59`) → `?add=new` | KEPT AS CODE | label **"Add somebody"** ("Invite someone" **REMOVED** — the control adds, the invitation is a second step; `TestEmployeeAdd_TheRosterOffersAWayIn`, `employeeadd_test.go:53`); keep the venueless "Add a venue first" branch (`staffform.templ:29-50`) |
| Status chips All · Active · Invited · Deactivated | status param, `panelStatuses` | EXISTS (links) + **BUILT (D3, M12-17)** counts | `<a href="?status=…">` keeping the other filters (`rosterQuery`), `aria-current`; the status `<select>` goes; counts honour venue/department/name filters; the kicker total is unfiltered |
| "Home venue" select | `filterSelect("filter-location", …)` (`roster.templ:442`) | EXISTS | label "Home venue", "All venues" |
| Department filter | `roster.templ:443` | KEPT AS CODE | render when the business has departments (KM needs it) |
| "Clear" | `roster.templ:446-448` | EXISTS — **keep** | |
| Roster card | rows are dockets today (`employees.templ:227-245`) | DERIVE | one paper card of rows |
| Venue group headers | list ordered by name | **REMOVED (D1)** | flat, name order; the venue sits in the row meta |
| Initials square | — | DERIVE | README §5 |
| Name | `RosterRowView.Name` | EXISTS | `truncate` |
| Job title | `employees.role` (`00003…sql:47`), not selected today | **BUILT (D3, M12-18)** | NULL → omit the "·" part |
| Venue in meta | `Venue` / `NoHome` | EXISTS | keep the three branches (name / "No home venue" / "Venue could not be read", `roster.templ:45-56`); department joins the meta when set |
| Status pill | `lifecycleTally` (`roster.templ:398-407`, `.tally--active/invited/deactivated`, `input.css:668-683`) | EXISTS (shape new) | database words; pill recipe README §5 |
| Last tap | — | **BUILT (D3, M12-18)** | LATERAL on `transactions_tenant_employee_occurred_idx`; "14:31 today" / "02:14 yesterday" / "Mon 09:02" in the tenant zone; `reject` → "refused"; practice rows excluded; "—" when none |
| Phone column | `Person.LiveDevices`, `deviceLine` (`employees.go:706-718`) | DERIVE | row: "1 signed in" / "none"; the full sentence in the card. Live **browser sessions**, not phones; deactivated people show their real count (X5) |
| Row selection | `ManageHref` "Manage" link (`roster.templ:82-91`) | DERIVE | the whole row is one `<a href={ManageHref}>`; `Selected` → `aria-current="true"`; the accessible name is the person's name ("Manage" goes) |
| Footer | `MoreHref` "Next page" (`employees.templ:232-244`) | KEPT AS CODE | "Next page" stays (pinned; comment `:233-242`); "Showing N of M" allowed with M12-17; "scroll for more" **REMOVED**; never hard-code the page size in comments (`TestComments_DoNotQuoteTheDriftingRosterSize`) |
| Empty states | `employees.templ:246-268`, `emptyRosterHeading` (`:290`) | EXISTS — **keep** | three branches + `Queried` gate |
| Zone line | "Dates shown in Europe/Malta" (`employees.templ:214-216`) | EXISTS — **keep** | may move into the footer |
| Done / Problem notices | `employees.templ:51-210` | EXISTS — **keep** | above the panes |
| **Card (drawer / top card / phone screen)** | `RosterActions` (`roster.templ:120-369`) | DERIVE | **one card with everything** (no new URL parameter): summary, activation box, last 7, phone line, invite, external ref and e-mail blocks, move form, "Enter a record by hand", deactivate two-step. Desk: aside. `md`–`desk`: above the list. Phone: card only, list hidden, "Back to People" to `CloseHref` (`rosterview.go:122`). |
| Card name / meta | `RosterActionsView.Name/Venue/Department/NoHome` | EXISTS + **BUILT (D3, M12-20)** | role and "since" from the per-person summary (a separate `GetPanelPersonSummary`) |
| Activation box (invited) | invite button (`roster.templ:144-195`, `inviteLabel` `employees.go:633`) | DERIVE + REUSE | "Invite created N days ago" from `ListPendingInvitesForEmployee` (`db/queries/invites.sql:302`, through the summary card M12-20); no pending invite → "No working link yet"; e-mail mode says "created", not "sent"; **Copy link REMOVED** (the code is not stored, §4.7); activation only on the **own** business's plaque (K-35) |
| Last 7 taps | — | **BUILT (D3, M12-19)** | day, dir + time, verdict **word** (no colour-only dot); `type` NULL → "—"; partner rows show only day/dir/time/word |
| Phone section | session count + dates | EXISTS + **BUILT (D3, M12-20)** | "1 signed in · since 3 Sep · last used …"; the activation venue **REMOVED** (sessions carry no location; the activation tap writes no record); `device_info` not printed |
| "Unlink phone" | no route | **REMOVED (D1)** | |
| Deactivate | two-step link → confirmation → POST with one-shot token (`roster.templ:315-342`, `employees.go:489-500`) | KEPT AS CODE | label "Deactivate"; keep "Asks first. It cannot be undone afterwards." (`:335`) and the confirm sentence (`:318-322`); "They can no longer tap" **REMOVED** (their taps are refused **and recorded**, §5 row 4) |
| Deactivated paragraph | `deactivated_at`; `roster.templ:338-340` | DERIVE | "Deactivated on 2 Oct. Their taps are refused, and every refusal is still recorded." |
| Tablet "This week 33:15 · +1 open" | — | **REMOVED (D1)** | |
| Tablet "Shift 18:00–02:00 · from Bar department" | — | **REMOVED (D1)** | lateness rule is department else tapped venue |
| Tablet "Edit details" | no handler (names are API-written, `employees.sql:951`) | **REMOVED (D1)** | |

### 1.3 Breakpoints

- Header `flex flex-wrap items-end justify-between gap-4`; phone add button `w-full min-h-[52px]`.
- Filter row `flex flex-wrap items-center justify-between gap-3`; phone chips scroller `-mx-4 px-4 [scrollbar-width:none]`.
- Panes `flex flex-col gap-6 desk:flex-row desk:items-start`; list `min-w-0 desk:flex-[999_1_560px]`;
  card `min-w-0 desk:flex-[1_1_340px] order-first desk:order-none` (at 864 px: 500 + 340 + gap fits).
- Row: desk `flex flex-wrap items-center gap-3.5 px-5 py-2.5 min-h-[60px]`, last tap `w-[104px] text-right`,
  phone column `w-[60px] text-right` from `md`; phone 64 px row, meta "role · last tap", chevron `md:hidden`.
- Mini dockets `grid grid-cols-[repeat(auto-fill,minmax(84px,1fr))] gap-x-2 gap-y-3`; `md:grid-cols-7` in the tablet card.

### 1.4 Recipes

Roster card `rounded-[18px] border border-line bg-paper py-2`; roster row, status pill, initials,
drawer, chips, danger button: README §5. Activation box `flex flex-col gap-3 rounded-xl border
border-dashed border-saffron bg-saffron/10 p-3.5`. Mini docket `.docket--mini` (`px-2 pt-2 pb-2.5
text-[13px]`, shared perforation kept; **sharp**); verdict word `border px-1 text-[10px] font-bold
uppercase tracking-[0.12em]` with the status border — a tally-sized word, not a stamp, no rotation.

### 1.5 Not ported (People)

header search by role/venue · "Invite someone" · "Stopped" and "Invited · waiting for first tap" ·
venue grouping and "N shown" · "scroll for more" · "Unlink phone" · "Copy link" · "Linked by tap at
<venue>" · "Unlinked when stopped" · masked e-mail · tablet "This week", "Shift", "Edit details" ·
colour-only verdict dots.

### 1.6 Binding copy (People)

| Code (binding) | Board | Port |
|---|---|---|
| "Add somebody" (`staffform.templ:55`) + "They are added as invited — you send them their activation link afterwards." (`:57`) | "Invite someone" | keep code |
| tally words INVITED / ACTIVE / DEACTIVATED (`roster.templ:401-405`) | Active / Invited · waiting … / Stopped | keep code |
| "No invitation can be sent to somebody who has been deactivated." (`roster.templ:192-194`) | — | keep |
| deactivate confirm (`roster.templ:318-322`), "Asks first. It cannot be undone afterwards." (`:335`) | "They can no longer tap. Their records stay." | keep code |
| "Next page" (`employees.templ:243`) | "scroll for more" | keep code |
| every Problem/Done sentence (`employees.templ:51-210`) | — | keep verbatim |

### 1.7 Tests

`internal/handler/employees_db_test.go`: `rosterRowRE` (`:203`, X8), `TestPanelEmployeesDB_ListsPeopleWhoHaveLEFT` (`:337`, words `:402-407`), the never-used-device case (`:1112`), the byte ceiling (`:865`), `…PagesThroughEverybodyExactlyOnce` (`:596`), `TestPanelEmployeesDB_ANameWithMarkupInItIsEscaped` (`:1173`, initials too), the no-session-secret case (`:537`).
`internal/handler/employees_test.go`: `TestEmployeesSection_ConditionalControlsCarryATouchTargetClass` (`:701`), `TestEmployeesSection_LoadsNoScript` (`:758`), `TestEmployeesSection_IsTheOnlySectionRenderingTheRoster` (`:802`), `TestPanelStatuses_AreTheDomainVocabularyAndNothingElse` (`:380`, chips from `panelStatuses`), the device-line table (`:350`), cursor and paging (`:250`, `:302`), `TestComments_DoNotQuoteTheDriftingRosterSize` (`:536`).
`internal/handler/employeeactions_test.go`: every-control-routes (`:150`), no action until a person is named (`:235`), `…OffersNoActionTheServerWouldRefuse` (`:294`), `TestEmployeeDeactivate_TheConfirmationStepIsENFORCED` (`:366`).
`internal/handler/inviteemail_test.go` (`:357`; `TestRosterActions_ThePanelModeCardIsPinned` `:389` renders `RosterActions` directly); `employeeemail_test.go` (`:220`); `employeeadd_db_test.go` (`:76`, `:170`).
New reads: evidence-reader entries; RLS DB test per query written **without** the tenant filter.

### 1.8 Red lines (People)

§4.7 — no invite code, session token, device label or e-mail field on `RosterRowView`
(`rosterview.go:9-16`); "Copy link" never implemented by storing the link · §4.5 — every new query has
`tenant_id` + RLS; `employee_id` only narrows inside the caller's tenant · §4.6 — unknown ≠ 0; empty
states gated on `Queried` · K-39 — the strip shows no trust / IP / GPS / note; a partner venue name only
via the ledger's site naming · §4.3 — nothing writes `transactions` · never log the e-mail.

---

## 2. Person form — Add and Manage (`StaffForm`)

### 2.1 Wiring

| Item | Where |
|---|---|
| Add form | `GET /admin/employees?add=new` (`employees.go:264-267`), rendered by `RosterAdd` (`staffform.templ:28`) |
| Add submit | `POST /admin/employees/add` (`employeeactions.go:107`) → `employeeAdd` (`:203`); refusals re-render via `addFormAgain` (`:344`) |
| Manage | the `?manage=<id>` card (`RosterActions`, `roster.templ:120`); posts `/admin/employees/{invite,deactivate,move,email,external-ref}` (`employeeactions.go:108-113`) |
| View models | `RosterAddView` (`staffformview.go:18`), `StaffFormView` (`:87`); `RosterActionsView` (`rosterview.go:109`) |
| SQL | `CreateEmployee` (`employees.sql:637`); `MoveEmployee` (`:549`); `SetEmployeeEmail` (`:784`); `SetEmployeeExternalRef` (`:826`) |

### 2.2 Add mode

| Element | Source | Status | Port note |
|---|---|---|---|
| Crumbs "People / Add somebody" | — | DERIVE | M12-01 Crumbs |
| Kicker "Add somebody · they activate with their first tap" / Title "New person" | `staffform.templ:79-80` | EXISTS (words) | keep `aria-label="Add somebody to this business"` on the section |
| Page layout | form is a disclosure inside the roster page (`employees.templ:222`) | DERIVE | same URL; when `Add.Open`, render the add layout without list and chips; keep the venueless branch and the roster-position `Hidden` fields (`staffform.templ:90`) |
| Name | `full_name` (`staffform.templ:91-107`) | EXISTS | keep the help "What your team calls them. This is the only thing we need." and the reserved note line |
| Venue + Department (2 columns) | `location_id` (`:108-136`), `department_id` (`:137-151`) | EXISTS | "Choose a venue…" placeholder stays (`TestEmployeeAdd_WithSeveralVenuesNothingIsChosenByDefault`, `employeeadd_test.go:771`); single venue pre-selected |
| Venue help | "Where they normally work. They can still tap in at any of your venues." (`staffform.templ:134`) | KEPT AS CODE | "Lateness is measured against this venue's shift" **REMOVED** (false: department, else the tapped venue — §5) |
| Department help | `staffform.templ:148-149` | EXISTS | full sentence at every width |
| Job title (optional) | `role` (`:152-168`) | EXISTS | keep "Nothing depends on it." |
| E-mail (optional) | `email` (`:169-191`) | EXISTS | keep the full help (the citext sentence binds, `:182-185`); `placeholder="name@example.com"` allowed |
| Billing note | `BillingNote` (`staffform.templ:192-198`; constants `employees.go:410-420`) | EXISTS | handler text, not the board's short copy |
| "Add them" + "Cancel" | `staffform.templ:199-202` | EXISTS | phone sticky submit via `form="add-person-form"` (the Account page's no-`form=` pin is local to Account) and `sticky bottom-[calc(68px+env(safe-area-inset-bottom))] md:static`; Cancel kept on phone |
| "After you add them" aside | — | DERIVE (static) | mode-aware step 1 (panel: "Show them their activation link"; e-mail mode: "We email their activation link; with no address the owner can show it on screen") via a new `InviteByEmail bool` on `StaffFormView` (set from `a.inviteMail != nil`, `employees.go:546`); step 3 "Their first tap on one of your plaques" (K-35); status word INVITED |

### 2.3 Manage mode

| Element | Source | Status | Port note |
|---|---|---|---|
| Header card | `RosterActions` header (`roster.templ:122-142`) | EXISTS + **BUILT (D3, M12-20)** | role, since, devices from the summary; panel-added invited rows have `invited_at` NULL (`employees.sql:721-733`) → print the pending-invite date or nothing |
| "Where" card | move form (`roster.templ:343-368`) | EXISTS | field names and the no-empty-venue rule (`:349-361`); "No department" option stays |
| "Invitation" card | `roster.templ:144-195`; mode by config (`employees.go:546-550`) | KEPT AS CODE | **never both buttons**: panel mode one `InviteLabel`; e-mail mode the e-mail button only with an address, "Show the link on screen" only for an owner with no address (`CanShowLink` `:548`, ADR 0022 §7); the retire sentence ("A new invitation retires every earlier link") may be added (true, migration 00012) |
| "Email on file" | full address (`roster.templ:211-219`) | KEPT AS CODE | masked `a•••@•••.mt` **REMOVED** |
| New address form | owner-only (`roster.templ:220-247`) | EXISTS | binding help "Saving a different address stops every activation link … not been used yet" (`:236-239`) |
| External ref | `roster.templ:258-297` | EXISTS | keep the format help |
| "Only the business owner can change …" | `roster.templ:246`, `:294` | DERIVE | one combined line only when **both** forms are hidden (manager) |
| "A missed tap?" + "Enter a record by hand" | `RecordHref` (`roster.templ:298-314`) | EXISTS | unconditional (also for deactivated, `rosterview.go:145-154`) |
| Deactivate card (tomato frame) | `roster.templ:315-342` | EXISTS | "Their own taps are refused from now on. Every record they made stays, and still counts." is true; keep the confirm step |

### 2.4 Breakpoints

Add: `desk:flex-row` form (`flex-[999_1_520px]`) + aside (`flex-[1_1_300px]`); `md` single column with
Venue/Department `grid-cols-2`; phone stacked with sticky submit. Manage: desk main column (header,
Where, Invitation, e-mail + ref `grid-cols-[repeat(auto-fit,minmax(240px,1fr))]`) + aside (missed tap,
deactivate); `md` header then `grid grid-cols-2 gap-4`; phone stacked (Invitation before Where).

### 2.5 Not ported (person form)

"Lateness is measured against this venue's shift" · both invite buttons at once · masked e-mail ·
shortened help texts on tablet/phone · "Edit details".

### 2.6 Tests

`employeeadd_test.go` (all `TestEmployeeAdd_*`, especially `TestEmployeeAdd_TheRosterOffersAWayIn` `:49`, rejected-form refill `:256`, single venue `:709`, `TestEmployeeAdd_WithSeveralVenuesNothingIsChosenByDefault` `:771`, labelled form `:941`, bill sentence `:688`, roster position `:728`); `employeeadd_db_test.go` (`:76`, `:170`, `:428`); `employeeemail_test.go` (`:220`, `:371`); `inviteemail_test.go` (`:357`, `:389`); `employeeactions_test.go` (`:294`, `:366`, `:1233`, `:1270`).

### 2.7 Red lines

Offering both invite buttons would offer actions the server refuses (`OffersNoActionTheServerWouldRefuse`,
`TheCardOffersOnlyWhatTheServerWouldDo`). The deactivation token flow stays bound to the person
(`employeeactions.go:798-832`).

---

## 3. Invite result — `AdminInviteIssued` / `AdminInviteEmailed`

### 3.1 Wiring

`POST /admin/employees/invite` (`employeeactions.go:108`) → `employeeInvite` (`:379`); e-mail mode
`employeeInviteByEmail` (`internal/handler/inviteemail.go:379`). Views `InviteIssuedView`
(`employeesview.go:135`, built at `employeeactions.go:470-477`) and `InviteEmailedView` (`:182`,
headings `inviteEmailedHeading` `:216`). Templates `AdminInviteIssued` (`employees.templ:451`),
`AdminInviteEmailed` (`:513`). No GET by design (`employeesview.go:126-134`).

### 3.2 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Crumbs "People / Ahmed Hassan / Invitation" | `BackHref` (`?manage=<id>` + roster position, `employeeactions.go:476`) | DERIVE | |
| Kicker "One person · one link · shown once" / Title "Invitation" | — | DERIVE | e-mail outcomes: kicker "Sent by email" (nothing is shown) |
| issued: notice "Invitation created for <name>" + "Give this link to … only to them" + validity | `employees.templ:453-459` | EXISTS — **binding** | the name **before** the credential (`employees.templ:448-450`) |
| "This link belongs to" + name + "role · venue" | `v.Name`; `person.LocationName` (`staff.go:187-206`) | EXISTS + DERIVE + **BUILT (D3, M12-20)** | add `Venue` to the view; role from the summary or omitted |
| Link box "Activation link — shown once" | `employees.templ:461-465` (text, `break-all font-mono`) | EXISTS | stays **text, not `<a>`** (`employeesview.go:142-147`); add `select-all` |
| "Copy" / "Copy link" | — | **REMOVED (D1)** | |
| "Shown as text, not a link, so it does not open here and sign your own browser in." | — | DERIVE | true, may be added |
| "Taptime keeps no copy …" + retire sentence | `employees.templ:466-490` | EXISTS | all three `Retired` branches (0 / 1 / >1) |
| sent: notice + "Sent to" + address | `employees.templ:515-540`, full `v.Address` | KEPT AS CODE | full address (`inviteemail_test.go:188`); "Sent, never delivered" sentence binds (`:505-507`); keep the Retired branches |
| unsent: alert | `employees.templ:541-548` | EXISTS | |
| "Send it again" | — | **REMOVED (D1)** | the card's invite button (via "Back to the list") does it |
| "Show a link instead" | — | **REMOVED (D1)** | the server answers "show-has-address" when an address exists |
| Other e-mail outcomes | `employees.templ:549-643` | EXISTS — **keep** | every branch and number (`TestInviteEmail_EveryOutcomeIsASentenceAndNoneCarriesALink`, `inviteemail_test.go:178`) |
| "What <name> does next" aside | — | DERIVE (static) | step 3 "Taps one of your plaques" (own business only, K-35/ADR 0026); "It writes no attendance record — the next tap is a normal check-in"; step 2 generic ("Confirms it is them"); "No app to install, nothing biometric." (true, §4.1); e-mail mode step 1 "Opens the email on their own phone" |
| "Back to the list" | `employees.templ:492-494`, `:645-647` | EXISTS | keep the `btn` class (or register the new class) |

### 3.3 Breakpoints and recipes

`desk:flex-row` main (`flex-[999_1_560px]`) + aside (`flex-[1_1_320px]`); `md` aside under the docket,
its list `grid-cols-3`; phone stacked, code box `text-[15px]`, name `text-[26px]`. Notices:
`PanelNotice` (M12-09). Docket `.docket px-7 pt-[26px] pb-[30px]`. Code box `select-all break-all
rounded-xl border-2 border-dashed border-ink/50 bg-porcelain px-4 py-3.5 font-mono text-base`.
Numbered step `grid grid-cols-[36px_1fr] gap-3`, number `size-9 rounded-[10px] border border-ink
font-mono font-bold`.

### 3.4 Not ported (invite)

Copy buttons · "Send it again" · "Show a link instead" · "any Kebab Factory plaque" (holding-unsafe) ·
masked address.

### 3.5 Binding copy

Keep verbatim: `employees.templ:455-458`, `:467-468`, `:480-489`, `:517-518`, `:524-526`, `:529-538`,
`:543-547`, every refusal sentence `:569-641`.

### 3.6 Tests

`employeeactions_test.go` `TestEmployeeInvite_ShowsTheLinkOnceAndNeverLogsIt` (`:1146`), `:1204`;
`employeeactions_db_test.go` (`:181`, `:263`, `:353`); `inviteemail_test.go` (`:178`, `:280`, `:326`,
`:430`); `inviteemail_db_test.go` (`:163`, `:417`).

### 3.7 Red lines

§4.7 — the activation URL appears only in `AdminInviteIssued`, never with an `href`, never in another
view, never logged (`panelLinkSink`, `employeeactions.go:526`). Address: render, never log.

---

## 4. Manual entry — `/admin/records/new`

### 4.1 Wiring

| Item | Where |
|---|---|
| Routes | `GET /admin/records/new` (`manualEntryHref` `manualentry.go:99`, `dashboard.go:104`) → `manualEntryForm` (`:153`); `POST /admin/records/new` (`dashboard.go:196`) → `manualEntryReview` (`:191`); `POST /admin/records` (`:103`, `dashboard.go:197`) → `manualEntryRecord` (`:282`) |
| Success | **303 to the record on The Pass** `/admin?date=<local day>&channel=manual` (`manualRecordReturn` `:658-663`) — no "Recorded" page |
| View model | `ManualEntryView` (`manualentryview.go:28`); `ManualDirectionView` (`:147`); `manualDirections` (`manualentry.go:143-146`) |
| Templates | `AdminManualEntry` (`manualentry.templ:44`), `manualFormStep` (`:127`), `manualConfirmStep` (`:247`) |
| Domain | `internal/domain/manual` (`Subject`: Name, LocationName, DepartmentName, NeedsVenue, Zone = the **business** zone, `manual.go:302-305`) |
| Tab | **The Pass** stays current (X2) |

### 4.2 Elements

| Element | Source | Status | Port note |
|---|---|---|---|
| Crumbs "People / Ana Silva / Manual entry" | `BackHref` (`employeesHref?manage=<id>`, `manualentry.go:704`) | DERIVE | |
| Kicker "Enter a record by hand · adds, never edits" / Title "Manual entry" | — | DERIVE | true (§4.3) |
| Step progress "1 Fill in · 2 Check" | `Confirming` | DERIVE | static `<ol aria-label="Steps">`, `aria-current="step"`; **no step 3** (D1) |
| Docket header: name, "no home venue" | `manualentry.templ:77-94` | EXISTS | role **REMOVED** (`Subject` has none); keep the "venue not recorded" branch |
| "Someone else?" | `employeesHref` | DERIVE | a plain link to People (registered class) |
| Deactivated warning | `manualentry.templ:95-109` | EXISTS — **keep** | `TestManualEntry_TellsTheManagerWhenSomebodyIsDeactivated` (`manualentry_test.go:406`) |
| Problem notice | `manualentry.templ:46-75` | EXISTS — **keep** | fix the pre-existing "this venue's own clock" in the "when" refusal (`:54`) to "this business's own clock" while porting |
| In / Out radio cards | radios (`manualentry.templ:130-152`) | DERIVE (markup) | real radios: `<label>` cards with a visually hidden `peer sr-only` input and `has-[:checked]:` / `peer-checked:` styles; `name="direction"`, values from `manualDirections`; labels **KEPT AS CODE** ("Check-in — started work" / "Checkout — stopped work"); visible focus ring |
| Venue select (no home venue) | `manualentry.templ:153-171` | EXISTS | |
| Partner venue select (holding) | `manualentry.templ:172-188` | EXISTS — **keep** | |
| Date + time | `manualentry.templ:189-212` | EXISTS | phone stacked |
| Zone line | "Times are this business's own clock: Europe/Malta" (`manualentry.templ:216-218`) | KEPT AS CODE | "In this venue's own clock" **REMOVED** (the zone is the business's) |
| "Why (optional)" | `manualentry.templ:219-233` | EXISTS | keep `maxlength` |
| "Review this record" + Cancel | `manualentry.templ:235` | KEPT AS CODE | label pinned (`manualentry_test.go:129`, `:419`); phone sticky via `form=` |
| "About to be recorded" box | `manualentry.templ:249-272` | EXISTS | both clocks (`:243-246`), the backdated sentence (`:263-268`), the partner-venue line (`:258-262`) |
| Permanence warning | `manualentry.templ:273-299` | EXISTS — **full at every width** | ADR 0011; `TestManualEntryConfirm_SaysBOTHMeasuredConsequencesAndOnlyWhereTheyApply` (`:832`) |
| "Record it" + Cancel | write form (`manualentry.templ:300-317`) | EXISTS | only inside `if v.Confirming` (`TestManualForm_NeverRendersTheWriteFormNoMatterWhatTheViewCarries`, `:654`) |
| "Change something" | — | **REMOVED (D1)** | Cancel stays; backlog |
| Step 3 "Recorded." / "See it in The Roll" / "Enter another" | — | **REMOVED (D1)** | the redirect lands on the record itself (`manualentry.go:645-652`) |
| Preview docket | — | DERIVE | **confirm step only**, `components.TransactionDocket` with `Manual: true` → APPROVED stamp + "Entered by a manager" tally (a manual record is `ok`, `transactions.sql:284`); the board's dashed "MANUAL" stamp **REMOVED** (stamps are verdicts only); "Entered by" = `PanelChrome.FullName`; written-at time omitted |
| Aside notes | — | DERIVE | "This adds a new record. Nothing existing is changed." / "Manual records are counted in hours and shown apart in The Roll and the CSV." |

### 4.3 Breakpoints and recipes

`desk:flex-row` docket (`flex-[999_1_520px]`) + aside (`flex-[1_1_320px]`); `md` step progress under the
title, preview `grid grid-cols-[280px_1fr]` below the docket; phone compact progress, preview reduced,
sticky action bar above the shell bar, date/time stacked. Radio card `relative flex min-h-16
cursor-pointer flex-col justify-center gap-0.5 rounded-xl border border-line px-3.5 py-2.5
has-[:checked]:border-2 has-[:checked]:border-ink has-[:checked]:bg-paper` (confirm the standalone CLI
supports `has-*`). Permanence alert: `PanelNotice` alert tone or `rounded-xl border border-tomato
bg-tomato/5 px-4 py-3.5`.

### 4.4 Not ported (manual)

"3 Recorded" step and its banner, "See it in The Roll", "Enter another" · "Change something" · live
preview on step 1 · dashed MANUAL stamp · role line · "In this venue's own clock" · shortened permanence
warning · "Checked in — started a shift" labels · "Check the record".

### 4.5 Tests

All of `manualentry_test.go` (especially `:114`, `:154`, `:222`, `TestManualEntryRecord_WritesWhatTheFormSaidAndLandsOnTheRecordItself` `:253`, `TestManualEntry_LandsOnAFilterTheSectionACCEPTS` `:304`, `:406`, `:654`, `:713` "Enter a record by hand" reachable from the roster, `:832`, `:875`, `:989` accent on native controls — revisit when radios become hidden, `:1027`); `manualvenue_test.go:20`.

### 4.6 Red lines

§4.3 permanent write; the warning precedes the write button (the `if v.Confirming` barrier) · §4.7
note/date/time never in a URL · §6 instants converted once; both clocks on screen · the holding
partner-venue control stays · the confirmation cookie is cleared only after the gate passes
(`manualentry.go:328-343`).
</content>
</invoke>
