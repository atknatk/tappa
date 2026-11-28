# App Store — Taptime staff app

> M16-27 / M16-28 working document (2026-11-25). Metadata, the App Privacy answers and the
> App Store Connect internal-testing runbook for `mt.taptime.app` (team `P6PM935BK2`). **No secret, address, e-mail or
> invitation link goes in this file** (the repository is public): the review notes' demo access
> and contact details are typed only into App Store Connect.
>
> Same gates as Google Play (`docs/store/google-play.md`): App Store Connect internal testing first; App Review waits for
> PR #49 on `main` (`/app/v1/account/delete`) and the lawyer-approved privacy text (M16-41).

## 1. What the build carries (measured 2026-11-25, unsigned Release build for `generic/platform=iOS`)

| Item | Value | Where |
|---|---|---|
| Bundle ids | `mt.taptime.app`, widget `mt.taptime.app.widgets` | `Taptime.xcodeproj` |
| Version | `MARKETING_VERSION` 1.0.0, `CURRENT_PROJECT_VERSION` 1 (project level, both targets) | |
| App icon | `Taptime/Assets.xcassets/AppIcon.appiconset`, one 1024 px universal image, no alpha — the launcher plaque on tappa-green | product: `Assets.car`, `CFBundleIconName = AppIcon` |
| Privacy manifests | `Taptime/PrivacyInfo.xcprivacy`, `TaptimeWidgets/PrivacyInfo.xcprivacy` | both copied into the product |
| Export compliance | `ITSAppUsesNonExemptEncryption = false` — HTTPS and the device-key signature only (authentication, exempt) | `Taptime/Info.plist` |
| Entitlements | associated domains `applinks:taptime.mt`, NFC reader formats `TAG` (App Store Connect refuses `NDEF` for a 15.0 target, ITMS-90778, measured 2026-11-27; the app opens only the NDEF reader session), App Group `group.mt.taptime.app`; no push | `Taptime/Taptime.entitlements` |
| AASA | live: `P6PM935BK2.mt.taptime.app`, paths `/t`, `/q` | `https://taptime.mt/.well-known/apple-app-site-association` |

## 2. Metadata

| Field | Value |
|---|---|
| Name (≤ 30) | Taptime: Clock In — "Taptime" is taken on the App Store (measured 2026-11-27, user decision the same day); the home-screen name stays Taptime |
| Subtitle (≤ 30) | Tap the plaque. You're in. |
| Primary language | English (U.K.); Turkish localisation |
| Category | Business |
| Price | Free |
| Support URL | https://taptime.mt |
| Marketing URL | https://taptime.mt |
| Privacy policy URL | https://taptime.mt/legal/privacy (replaced by the approved text before App Review, same URL) |
| Copyright | 2026 Everva Bilisim A.S. |
| Age rating | every questionnaire answer *None / No* → 4+ |
| Keywords (≤ 100) | `time clock,attendance,clock in,timesheet,shift,NFC,staff,hours,workplace,punch` |

**Promotional text (≤ 170)**

- en: `Clock in by tapping the plaque at your workplace. No fingerprints, no tracking — your location is read only at the moment you tap.`
- tr: `İş yerindeki plakete dokunarak giriş yap. Parmak izi yok, takip yok — konumun yalnız dokunduğun anda okunur.`

**Description** — the Google Play full description (`docs/store/google-play.md` §1), word for
word, both languages, with one line added after the feature list:

- en: `• On iPhone, a home-screen widget and a lock-screen counter (iOS 16.2 or later) show your time at work`
- tr: `• iPhone'da ana ekran widget'ı ve kilit ekranı sayacı (iOS 16.2 ve sonrası) işteki süreni gösterir`

**Screenshots** — required: 6.9" iPhone (1320 × 2868) or 6.5" (1284 × 2778). Taken on a device
with a **demo business**, never a real employee (user decision 2026-11-25). Not taken yet.

## 3. App Privacy (the label)

Mirrors `Taptime/PrivacyInfo.xcprivacy`. Every answer: **not used for tracking**.

| Data type | Collected | Linked to the user | Purpose |
|---|---|---|---|
| Location — Precise Location | Yes | Yes | App Functionality |
| Location — Coarse Location (the server stores each tap's source IP and matches it to the workplace network, `internal/handler/apptap.go` `SourceIP`) | Yes | Yes | App Functionality |
| Identifiers — Device ID | Yes | Yes | App Functionality |
| User Content — Other User Content (clock in/out, a forgotten clock-out's time) | Yes | Yes | App Functionality |
| Contact info, health, financial, contacts, browsing, search, purchases, diagnostics, usage data, sensitive info | No | — | — |

No third-party SDK, no analytics, one host (`taptime.mt`). Location is one foreground reading at
the press (CLAUDE.md §4.2); the Info.plist strings say so.

## 4. App Review notes (draft 2026-11-28 — values only in App Store Connect)

The Phase H path (ADR 0051; Q55 (b); M16-52, M16-53, M16-56). M16-57 moves the final text into
`mobile/README.md` → *Store review* once the demo business exists, and checks every step on a device.
`<…>` are placeholders: the real values are typed only into App Store Connect.

**Notes field (English, as pasted):**

```
Taptime is a commercial time-and-attendance service that businesses subscribe to on the web
(Guideline 3.1.3(c)). It is not an internal app for one organisation: any business can open an
account at taptime.mt/signup, and its staff then use this app. There is nothing to buy in the app.

Review path (one iPhone, no NFC plaque needed):
1. Open the app and tap Sign in. E-mail: <DEMO_EMPLOYEE_EMAIL>  Password: <DEMO_EMPLOYEE_PASSWORD>
2. Read the short notice and continue.
3. The app asks you to tap your workplace's plaque. Instead, scan the attached QR image
   (<QR_ATTACHMENT>) with the iPhone Camera. This demo business is flagged for review, so its
   printed QR can link a phone; at a real workplace only the plaque, a live venue screen or a
   manager's approval can.
4. Tap Clock in. The record is saved as "flagged" for the manager to check, because a printed QR
   does not prove that you are at the venue. That is the expected result.
5. My hours shows your record. Settings > Delete my account sends a deletion request
   (Guideline 5.1.1(v)).

"Create a business account" opens the web sign-up inside the app. It checks a real EU VAT number,
so it may stop at that step; you do not need it to review the staff app.
Location is read once, at the moment you tap Clock in. The app never reads it in the background.
```

**If App Review answers under Guideline 3.2 (*"limited audience"* / *"intended for a specific
business"*) — Resolution Center reply (draft):**

```
Taptime is not built for one organisation. It is a multi-tenant product that any business can buy:
employers sign up at taptime.mt/signup and invite their own staff. Accounts are created by each
employer, which
Guideline 3.1.3(c) allows for services sold directly to organisations; there are no purchases in
the app. The review account belongs to our demo business; we are happy to set up another one.
```

If the rejection stands after the reply, the next options (Apple's *Unlisted App Distribution*, or
Apple Business Manager custom apps) are a user decision (M16-57 → *Rejection risks*).

## 5. App Store Connect internal-testing runbook

Signing and the first install are the user's (Xcode, automatic signing, team `P6PM935BK2`).

1. Xcode → both targets (Taptime, TaptimeWidgets) → Signing & Capabilities: team `P6PM935BK2`,
   *Automatically manage signing*, App Groups `group.mt.taptime.app` ticked.
2. App Store Connect → Apps → **+** → New App: iOS, name *Taptime: Clock In*, primary language English (U.K.),
   bundle id `mt.taptime.app`, SKU `taptime-ios`. **Done 2026-11-27** (Apple ID 6840191846, team
   Everva Bilişim Anonim Şirketi).
3. Xcode → destination *Any iOS Device (arm64)* → Product → Archive → Distribute App →
   App Store Connect → Upload. From the command line (how build 1.0.0 (1) went up, 2026-11-27):
   `xcodebuild archive` then `xcodebuild -exportArchive` with an export options plist
   (`method` app-store-connect, `destination` upload, `teamID` P6PM935BK2, automatic signing), both
   with `-allowProvisioningUpdates` and the team's App Store Connect API key
   (`-authenticationKeyPath/-KeyID/-KeyIssuerID`). The key is team-wide and lives in Infisical
   (project `optiway`, `prod`, `/brands/optiway/ios`: `ASC_API_KEY_P8_BASE64`, `ASC_API_KEY_ID`,
   `ASC_API_ISSUER_ID`); the `.p8` is written to a private temporary file only for the command.
4. App Store Connect → the app's beta-testing tab (Apple's beta service; its product name fits the
   repository's test-name pattern, `cmd/tappa/testnames_test.go:95`, so it is not spelled here) → the build → export compliance is answered by the Info.plist
   key → add the internal testers group. Internal testing needs no review; external testing
   needs Beta App Review and the §4 notes. **Done 2026-11-28:** internal group *Everva*,
   automatic distribution on (every Xcode upload reaches it; the setting cannot be changed later),
   two team users invited (names and addresses only in App Store Connect); build 1.0.0 (1)
   *Ready to Test*, which expires 90 days after upload.
5. Next build: raise `CURRENT_PROJECT_VERSION` (and `MARKETING_VERSION` for a new release).
   App Store Connect refuses a build number it has already seen for that version.
