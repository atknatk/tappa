# Google Play — Taptime staff app

> M16-27 / M16-28 working document (2026-11-25). Store text, the Data safety answers and the
> release runbook for `mt.taptime.app`. **No secret, address, e-mail or invitation link goes in
> this file** (the repository is public): reviewer access and contact details are typed only into
> Play Console.
>
> Status: internal testing first (user decision, 2026-11-25). On the internal-testing track:
> `1 (1.0.0)` 2026-11-25, `2 (1.0.0)` 2026-11-27 (Taptime mark from `main`). Production waits for two user
> decisions: PR #49 to `main` (the account-deletion endpoint `/app/v1/account/delete` exists only
> there) and the lawyer-approved privacy text (`docs/legal/privacy-draft.md`, M16-41).

## 1. Store listing

| Field | Value |
|---|---|
| App name | Taptime: Clock In — the App Store name (user decision 2026-11-27: "Taptime" is taken there; one name for both stores) |
| Default language | English (United Kingdom) — Malta market; Turkish added as a translation |
| App or game | App |
| Free or paid | Free |
| Category | Business |
| Website | https://taptime.mt |
| Privacy policy | https://taptime.mt/legal/privacy (the page that is live today; it is replaced by the approved text before production, same URL) |
| Contact e-mail | typed in Play Console only |

### Short description (≤ 80)

- **en:** Clock in by tapping your workplace plaque. No fingerprints, no tracking.
- **tr:** İş yerindeki plakete dokunarak giriş yap. Parmak izi yok, takip yok.

### Full description (≤ 4000)

**en**

```
Taptime is the staff app for workplaces that use Taptime plaques.

Hold your phone to the plaque at the door and you are clocked in. Tap again when you leave. That's it.

• Tap a plaque to clock in or out — no need to open the app first
• See at a glance whether you are clocked in
• Forgot to clock out? Tell your manager the time from your phone
• My hours: your own weekly hours and records, when your employer turns it on
• An optional reminder if you may have forgotten to clock out
• An optional live counter on the lock screen while you are at work

Private by design
• Your location is read only at the moment you tap — never in the background
• No fingerprints, no face scans
• No ads, no analytics, no tracking
• You can ask to delete your account at any time from Settings. Your clock-in records stay with your employer, because they are legal records.

Taptime needs an invitation from your employer. If your workplace does not use Taptime yet, ask your manager or visit taptime.mt.
```

**tr**

```
Taptime, Taptime plaketi kullanan iş yerlerinin çalışan uygulamasıdır.

Telefonunu kapıdaki plakete değdir, girişin yapılır. Çıkarken bir kez daha dokun. Bu kadar.

• Plakete dokunarak giriş ya da çıkış yap — önce uygulamayı açman gerekmez
• İşte olup olmadığını tek bakışta gör
• Çıkışı mı unuttun? Saatini telefonundan müdürüne bildir
• Saatlerim: kendi haftalık saatlerin ve kayıtların (işverenin açtığında)
• İsteğe bağlı: çıkışı unutmuş olabileceğinde bir hatırlatma
• İsteğe bağlı: işteyken kilit ekranında canlı sayaç

Gizlilik baştan
• Konumun yalnız dokunduğun anda okunur — arka planda asla
• Parmak izi yok, yüz taraması yok
• Reklam yok, analiz yok, takip yok
• Hesabının silinmesini istediğin an Ayarlar'dan isteyebilirsin. Giriş-çıkış kayıtların işvereninde kalır, çünkü bunlar hukuki kayıtlardır.

Taptime için işvereninden bir davet gerekir. İş yerin henüz Taptime kullanmıyorsa müdürüne sor ya da taptime.mt adresine bak.
```

### Graphics

Produced from the brand palette (`tappa-brand`), not committed (binary):

- App icon 512 × 512 — the launcher plaque (`res/drawable/ic_launcher_foreground.xml`) on tappa-green, drawn at 512 px.
- Feature graphic 1024 × 500 — plaque mark + "taptime" + "PUNCHLESS TIME & ATTENDANCE".
- Phone screenshots 1080 × 2160 (Play: the long side at most twice the short side; the device's 1080 × 2340 is cropped by the status and navigation bars): Today, Missing times, My hours, Settings — English. Taken with a **demo business**, never a real employee or venue (user decision 2026-11-25); the first internal-testing upload carries none.

## 2. App content declarations

| Declaration | Answer | Basis |
|---|---|---|
| Ads | No ads | no SDK (`mobile/android/README.md` → Dependencies) |
| App access | Some functionality is restricted: the app needs an employer invitation. Reviewer access (a demo-business invitation and the steps) is typed into Play Console only (M16-39) | |
| Target audience | 16–17 and 18+ (employees; no group under 13, so no Families policy) | |
| Content rating (IARC) | Category *Utility, Productivity, Communication or Other*; every content question *No*; no user-to-user content; no purchases | |
| News app / Government app / Financial features / Health | No / No / No / No | |

### Data safety

Every request goes to `https://taptime.mt` only, over HTTPS (cleartext off, redirects not
followed). No third-party SDK, no Play services, no analytics, no crash reporting. Inventory
measured 2026-11-25 from `mobile/android/app/src/main/kotlin/mt/taptime/app/wire/` (`Wire.kt`,
`AppClient.kt`, `Transport.kt`), `tap/Fix.kt`, `tap/TapLocation.kt` and the merged manifest.

| Data type | Collected | Shared | Required / optional | Purpose | Where |
|---|---|---|---|---|---|
| Location — precise | Yes | No | Optional (the tap is recorded without it, for review) | App functionality, fraud prevention / security | `lat`, `lng` on `POST /app/v1/tap`, one foreground reading at the press; `mockLocation` only when true |
| Location — approximate | Yes | No | **Required** | App functionality, fraud prevention / security | the **server** stores each tap's source IP linked to the person and matches it to the workplace network (`internal/handler/apptap.go` `SourceIP` → `transactions.source_ip`, decision row 6) — independent of any permission; also `lat`/`lng` when only coarse is granted |
| App activity — other actions | Yes | No | Required | App functionality | clock in/out (`want`), forgotten clock-out time (`at` on `/app/v1/claims/send`), hours viewed |
| Device or other IDs | Yes | No | Required | App functionality, security | server-issued device id and the device's P-256 public key (`App-Device-Id`, `publicKey`) |
| Personal info — name | No — the app only **receives** the employer's record of the name (`GET /app/v1/me`) and sends none; same answer as the App Store label | — | — | — | |
| E-mail, phone, contacts, photos, files, audio, health, financial, messages, web history, crash logs, diagnostics, advertising ID | No | — | — | — | not found in the app's code |

- Encrypted in transit: **Yes**.
- Users can request deletion: **Yes** — Settings → *Delete my account* (`POST /app/v1/account/delete`). Clock-in records are kept by the employer as legal records (§4.3); the request is passed to the employer.
- **Deletion web resource** (Play asks for a URL that works without the app): `https://taptime.mt/legal/privacy`, once the approved text with section 12's *"Without the app"* paragraph is published (`docs/legal/privacy-draft.md`, M16-41). The live page has no such paragraph today, so the field stays empty for internal testing — a **production prerequisite**, with PR #49 and the reviewer's demo invitation (M16-39).
- Accounts: created by the employer outside the app; the app binds a device to an invitation.
- `User-Agent`: the app sets none; Android's default one (OS version, phone model) goes with each request — **not measured** on the wire.

## 3. Release runbook

Prerequisites on the uploading machine (never in the repository): the upload keystore
(PKCS12, alias `upload`, certificate SHA-256 `8B:BD:17:14:B0:85:C8:CD:58:E2:6B:9E:63:3B:35:90:5C:6B:4C:BE:F5:D4:67:75:42:94:09:B8:28:3D:AC:58`,
the value Play Console shows under App signing → Upload key) and its password. **The source of
truth is Infisical** (user rule, 2026-11-27): project `shared-infra`, env `prod`, folder
`/tappa-mobile`, keys `TAPTIME_ANDROID_UPLOAD_KEYSTORE_B64` and `TAPTIME_ANDROID_UPLOAD_PASSWORD`
(that folder, not `/tappa`, which feeds the cluster). A machine keeps a working copy only: the
keystore at `~/.taptime-signing/taptime-upload.jks` and the password in the keychain (service
`taptime-android-upload`, account `upload`).

1. Raise `versionCode` (and `versionName`) in `mobile/android/app/build.gradle.kts`. Play refuses a code it has already seen.
2. Build the bundle:
   ```bash
   cd mobile/android
   export JAVA_HOME=/usr/local/opt/openjdk@21 ANDROID_HOME=/usr/local/share/android-commandlinetools
   # Apple silicon: JAVA_HOME=/opt/homebrew/opt/openjdk@21 ANDROID_HOME=$HOME/Library/Android/sdk
   # (versionCode 2 was built that way with Homebrew Gradle 9.9.2, 2026-11-27)
   export TAPTIME_UPLOAD_KEYSTORE=<absolute path to the upload keystore, outside the repository>
   gradle --no-daemon bundleRelease   # with TAPTIME_UPLOAD_PASSWORD set for this command only (below)
   jarsigner -verify app/build/outputs/bundle/release/app-release.aab   # "jar verified."
   ```
   The password goes into `TAPTIME_UPLOAD_PASSWORD` as a prefix of that one `gradle` command,
   its value read from the keychain by `security find-generic-password -a upload -s taptime-android-upload -w`
   — never exported, never typed, so it stays out of the shell history; `--no-daemon` keeps it out of a
   long-lived Gradle daemon's environment. Without it the bundle is unsigned and `jarsigner` says so.
   The release `BASE_URL` is pinned to `https://taptime.mt`; `-Ptaptime.baseUrl` stops a release build.
3. Play Console → Testing → Internal testing → Create release → upload `app-release.aab`.
4. **First upload only:** Play Console → Setup → App integrity → App signing → copy the **app signing key** SHA-256 (read 2026-11-27 from that page's Digital Asset Links block: `B9:F1:82:3F:20:76:55:43:2B:60:98:0D:EF:DE:BD:4C:94:E7:AD:FC:85:E5:89:1C:67:6C:A8:6F:5F:63:D2:73`; a public value, `assetlinks.json` serves it). Production's `TAPPA_APP_ANDROID_SIGNER_SHA256` takes that one value only (M16-08; the upload key's digest never goes to production). Setting it is a deploy — user decision. Until then `/.well-known/assetlinks.json` answers 404 and a plaque tap opens the browser instead of the app.
5. Promote internal → production only after the server the app needs is live (`/app/v1` is backwards compatible: an older app keeps working against a newer server; a newer app needs every endpoint it calls).
6. Roll back: Play cannot un-publish a version code. Halt the staged rollout, or ship the previous code base with a higher `versionCode`.
