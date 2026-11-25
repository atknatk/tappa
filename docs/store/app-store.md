# App Store — Taptime staff app

> M16-27 / M16-28 working document (2026-11-25). Metadata, the App Privacy answers and the
> TestFlight runbook for `mt.taptime.app` (team `P6PM935BK2`). **No secret, address, e-mail or
> invitation link goes in this file** (the repository is public): the review notes' demo access
> and contact details are typed only into App Store Connect.
>
> Same gates as Google Play (`docs/store/google-play.md`): TestFlight first; App Review waits for
> PR #49 on `main` (`/app/v1/account/delete`) and the lawyer-approved privacy text (M16-41).

## 1. What the build carries (measured 2026-11-25, unsigned Release build for `generic/platform=iOS`)

| Item | Value | Where |
|---|---|---|
| Bundle ids | `mt.taptime.app`, widget `mt.taptime.app.widgets` | `Taptime.xcodeproj` |
| Version | `MARKETING_VERSION` 1.0.0, `CURRENT_PROJECT_VERSION` 1 (project level, both targets) | |
| App icon | `Taptime/Assets.xcassets/AppIcon.appiconset`, one 1024 px universal image, no alpha — the launcher plaque on tappa-green | product: `Assets.car`, `CFBundleIconName = AppIcon` |
| Privacy manifests | `Taptime/PrivacyInfo.xcprivacy`, `TaptimeWidgets/PrivacyInfo.xcprivacy` | both copied into the product |
| Export compliance | `ITSAppUsesNonExemptEncryption = false` — HTTPS and the device-key signature only (authentication, exempt) | `Taptime/Info.plist` |
| Entitlements | associated domains `applinks:taptime.mt`, NFC NDEF, App Group `group.mt.taptime.app`; no push | `Taptime/Taptime.entitlements` |
| AASA | live: `P6PM935BK2.mt.taptime.app`, paths `/t`, `/q` | `https://taptime.mt/.well-known/apple-app-site-association` |

## 2. Metadata

| Field | Value |
|---|---|
| Name (≤ 30) | Taptime |
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

## 4. App Review notes (outline — values only in App Store Connect)

- The app needs an employer invitation; the reviewer gets a **demo business** invitation link
  and the steps (open the link → consent → the review path without a plaque, M16-23/M16-39).
  Creating that invitation in production is a user decision.
- Account deletion: Settings → *Delete my account* (Guideline 5.1.1(v)).
- NFC: the plaque URL opens the app (Universal Link); without a plaque the reviewer uses the
  live-QR / manager-approval path.

## 5. TestFlight runbook

Signing and the first install are the user's (Xcode, automatic signing, team `P6PM935BK2`).

1. Xcode → both targets (Taptime, TaptimeWidgets) → Signing & Capabilities: team `P6PM935BK2`,
   *Automatically manage signing*, App Groups `group.mt.taptime.app` ticked.
2. App Store Connect → Apps → **+** → New App: iOS, name Taptime, primary language English (U.K.),
   bundle id `mt.taptime.app`, SKU `taptime-ios`.
3. Xcode → destination *Any iOS Device (arm64)* → Product → Archive → Distribute App →
   App Store Connect → Upload.
4. App Store Connect → TestFlight → the build → export compliance is answered by the Info.plist
   key → add the internal testers group. Internal testing needs no review; external testing
   needs Beta App Review and the §4 notes.
5. Next build: raise `CURRENT_PROJECT_VERSION` (and `MARKETING_VERSION` for a new release).
   App Store Connect refuses a build number it has already seen for that version.
