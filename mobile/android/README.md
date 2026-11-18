# Taptime staff app — Android (M16-11 scaffold, M16-12 activation)

The optional native staff app decided in
[ADR 0045](../../docs/adr/0045-istege-bagli-native-uygulama.md), Android half. M16-11 was
the **scaffold** (the closed permission list, no backup, the build and test targets);
M16-12 adds the **device key and the activation** (below). The tap screen is M16-13's; the
browser path stays the default and is unchanged.

User-facing name **Taptime**, package and applicationId **`mt.taptime.app`** (Q35,
2026-11-14). It is a different app from the plaque relay in [`android/`](../../android/README.md)
(`mt.taptime.relay`, ADR 0017): separate Gradle build, no shared module.

## Toolchain — measured on this machine (2026-11-18)

The relay's measured chain ([`android/README.md`](../../android/README.md#toolchain--measured-on-this-machine-2026-09-18)),
re-measured for this build:

| Part | Version | Note |
|---|---|---|
| JDK | **21.0.10** (Homebrew `openjdk@21`) | `make mobile-android*` pins `JAVA_HOME` through the Makefile's `ANDROID_ENV` |
| Gradle | **9.7.1** (system, `/usr/local/bin/gradle`) | **no wrapper JAR in the tree** — this repository carries no binaries |
| Android Gradle Plugin | **9.4.1** | Kotlin is built in (KGP **2.2.10**); no `org.jetbrains.kotlin.android` plugin |
| Compose compiler | `org.jetbrains.kotlin.plugin.compose` **2.2.10** | a Kotlin **compiler** plugin, same version as the built-in KGP; build time only, nothing of it is in the APK |
| compileSdk / targetSdk | **36** | `platforms;android-36` |
| minSdk | **26** | |
| Build Tools | 36.0.0 | |

**One measured deviation, in the dependency version, not in the toolchain.** The newest
Compose BOM, **2026.09.00** (Compose 1.12.1), fails `checkDebugAarMetadata`: eight
Compose artefacts declare `minCompileSdk=37`. Compose 1.12.0 (BOM 2026.08.00) carries
the same (`ui-android-1.12.0.aar`, `aar-metadata.properties`). The build therefore uses
**BOM 2026.06.01 (Compose 1.11.4, `minCompileSdk=35`)**, the newest that builds on the
measured compileSdk 36. Going to Compose 1.12 means installing `platforms;android-37`
and re-measuring the chain; that is a toolchain change of its own.

## Build, install, test

```bash
make mobile-android        # assembleDebug; installs with adb only when a device is attached
make mobile-android-test   # JVM tests, including the merged-manifest measurement

# or by hand
cd mobile/android
JAVA_HOME=/usr/local/opt/openjdk@21 ANDROID_HOME=/usr/local/share/android-commandlinetools \
  gradle assembleDebug testDebugUnitTest
```

Neither target is part of `make check`. Memory caps are the relay's
(`gradle.properties`: `-Xmx2g`, no parallel build, two workers).

**Signing.** No `signingConfig` is declared: the debug build is signed with Gradle's own
per-machine debug key (`~/.android/debug.keystore`). Release signing is M16-28. This
repository is public; `.gitignore` here ignores `*.keystore` / `*.jks`, and
`scripts/redline-check.sh` S2 fails if such a file is tracked anyway.

Debug APK size, first clean build (2026-11-18): **9 027 870 B** (no minification; the
relay is ~0.9 MB because it has no runtime library at all).

## Dependencies (Q36, 2026-11-14)

Declared, runtime: exactly the Q36 set and nothing else.

```kotlin
implementation(platform("androidx.compose:compose-bom:2026.06.01"))
implementation("androidx.compose.ui:ui")
implementation("androidx.compose.foundation:foundation")
implementation("androidx.activity:activity-compose:1.13.0")   // not in the BOM
```

No `material3` (brand components are drawn by hand, skill `tappa-brand`), no QR library,
no Play Integrity (Faz E). Test only: `junit:junit:4.13.2` (the relay's measured
version); the manifest test reads XML with the JDK's own parser.

### `debugRuntimeClasspath` — what the APK really carries

Measured 2026-11-18 with `gradle :app:dependencies --configuration debugRuntimeClasspath`
and, as a cross-check, the configuration's `resolutionResult.allComponents`: the two
lists are the same **100 modules** (including the BOM, which is a platform and ships no
code). Every one of them is pulled in by the four declarations above, or by AGP's
built-in Kotlin (`kotlin-stdlib`).

Where the less obvious ones come from:

- `androidx.lifecycle:*` 2.9.4 — **transitive**, through `activity-compose` and Compose
  `ui`. Q36 says lifecycle "only if needed" as a *declared* dependency; none is
  declared, the transitive set cannot be avoided with activity-compose.
- `androidx.transition:transition` 1.6.0 → `dynamicanimation` → `legacy-support-core-utils`
  → `documentfile`, `loader` (→ `lifecycle-livedata`), `localbroadcastmanager`, `print` —
  **transitive through `androidx.compose.ui:ui-android` 1.11.4**.
- `androidx.emoji2`, `androidx.startup`, `androidx.profileinstaller`,
  `androidx.window` — transitive through Compose `ui` / `foundation` and `activity`.
- `kotlinx-coroutines` 1.9.0 and `kotlinx-serialization-core` 1.7.3 (with their BOMs) —
  transitive through Compose runtime and `androidx.savedstate`.

The full list (resolved versions):

```
androidx.activity:activity-compose:1.13.0
androidx.activity:activity-ktx:1.13.0
androidx.activity:activity:1.13.0
androidx.annotation:annotation-experimental:1.4.1
androidx.annotation:annotation-jvm:1.9.1
androidx.annotation:annotation:1.9.1
androidx.arch.core:core-common:2.2.0
androidx.arch.core:core-runtime:2.2.0
androidx.autofill:autofill:1.0.0
androidx.collection:collection-jvm:1.5.0
androidx.collection:collection-ktx:1.5.0
androidx.collection:collection:1.5.0
androidx.compose:compose-bom:2026.06.01
androidx.compose.animation:animation-android:1.11.4
androidx.compose.animation:animation-core-android:1.11.4
androidx.compose.animation:animation-core:1.11.4
androidx.compose.animation:animation:1.11.4
androidx.compose.foundation:foundation-android:1.11.4
androidx.compose.foundation:foundation-layout-android:1.11.4
androidx.compose.foundation:foundation-layout:1.11.4
androidx.compose.foundation:foundation:1.11.4
androidx.compose.runtime:runtime-android:1.11.4
androidx.compose.runtime:runtime-annotation-android:1.11.4
androidx.compose.runtime:runtime-annotation:1.11.4
androidx.compose.runtime:runtime-retain-android:1.11.4
androidx.compose.runtime:runtime-retain:1.11.4
androidx.compose.runtime:runtime-saveable-android:1.11.4
androidx.compose.runtime:runtime-saveable:1.11.4
androidx.compose.runtime:runtime:1.11.4
androidx.compose.ui:ui-android:1.11.4
androidx.compose.ui:ui-geometry-android:1.11.4
androidx.compose.ui:ui-geometry:1.11.4
androidx.compose.ui:ui-graphics-android:1.11.4
androidx.compose.ui:ui-graphics:1.11.4
androidx.compose.ui:ui-text-android:1.11.4
androidx.compose.ui:ui-text:1.11.4
androidx.compose.ui:ui-unit-android:1.11.4
androidx.compose.ui:ui-unit:1.11.4
androidx.compose.ui:ui-util-android:1.11.4
androidx.compose.ui:ui-util:1.11.4
androidx.compose.ui:ui:1.11.4
androidx.concurrent:concurrent-futures:1.1.0
androidx.core:core-ktx:1.18.0
androidx.core:core-viewtree:1.0.0
androidx.core:core:1.18.0
androidx.customview:customview-poolingcontainer:1.0.0
androidx.documentfile:documentfile:1.0.0
androidx.dynamicanimation:dynamicanimation:1.0.0
androidx.emoji2:emoji2:1.4.0
androidx.graphics:graphics-path:1.0.1
androidx.interpolator:interpolator:1.0.0
androidx.legacy:legacy-support-core-utils:1.0.0
androidx.lifecycle:lifecycle-common-jvm:2.9.4
androidx.lifecycle:lifecycle-common:2.9.4
androidx.lifecycle:lifecycle-livedata-core-ktx:2.9.4
androidx.lifecycle:lifecycle-livedata-core:2.9.4
androidx.lifecycle:lifecycle-livedata:2.9.4
androidx.lifecycle:lifecycle-process:2.9.4
androidx.lifecycle:lifecycle-runtime-android:2.9.4
androidx.lifecycle:lifecycle-runtime-compose-android:2.9.4
androidx.lifecycle:lifecycle-runtime-compose:2.9.4
androidx.lifecycle:lifecycle-runtime-ktx-android:2.9.4
androidx.lifecycle:lifecycle-runtime-ktx:2.9.4
androidx.lifecycle:lifecycle-runtime:2.9.4
androidx.lifecycle:lifecycle-viewmodel-android:2.9.4
androidx.lifecycle:lifecycle-viewmodel-ktx:2.9.4
androidx.lifecycle:lifecycle-viewmodel-savedstate-android:2.9.4
androidx.lifecycle:lifecycle-viewmodel-savedstate:2.9.4
androidx.lifecycle:lifecycle-viewmodel:2.9.4
androidx.loader:loader:1.0.0
androidx.localbroadcastmanager:localbroadcastmanager:1.0.0
androidx.navigationevent:navigationevent-android:1.0.0
androidx.navigationevent:navigationevent-compose-android:1.0.0
androidx.navigationevent:navigationevent-compose:1.0.0
androidx.navigationevent:navigationevent:1.0.0
androidx.print:print:1.0.0
androidx.profileinstaller:profileinstaller:1.4.0
androidx.savedstate:savedstate-android:1.3.2
androidx.savedstate:savedstate-compose-android:1.3.2
androidx.savedstate:savedstate-compose:1.3.2
androidx.savedstate:savedstate-ktx:1.3.2
androidx.savedstate:savedstate:1.3.2
androidx.startup:startup-runtime:1.1.1
androidx.tracing:tracing:1.2.0
androidx.transition:transition:1.6.0
androidx.versionedparcelable:versionedparcelable:1.1.1
androidx.window:window-core-android:1.5.0
androidx.window:window-core:1.5.0
androidx.window:window:1.5.0
com.google.guava:listenablefuture:1.0
org.jetbrains:annotations:23.0.0
org.jetbrains.kotlin:kotlin-stdlib:2.2.10
org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0
org.jetbrains.kotlinx:kotlinx-coroutines-bom:1.9.0
org.jetbrains.kotlinx:kotlinx-coroutines-core-jvm:1.9.0
org.jetbrains.kotlinx:kotlinx-coroutines-core:1.9.0
org.jetbrains.kotlinx:kotlinx-serialization-bom:1.7.3
org.jetbrains.kotlinx:kotlinx-serialization-core-jvm:1.7.3
org.jetbrains.kotlinx:kotlinx-serialization-core:1.7.3
org.jspecify:jspecify:1.0.0
```

No Google Play services artefact, no network client library, no analytics.

## Permissions — the merged manifest, not the source

`src/main/AndroidManifest.xml` asks for **NFC, INTERNET, ACCESS_FINE_LOCATION,
ACCESS_COARSE_LOCATION** and nothing else (CLAUDE.md §4.2: the location permissions
serve a one-shot, foreground reading at the tap, M16-13; they are requested at the tap,
not at start). What a phone grants is the **merged** manifest, every library's folded
in, so that is what is measured:

- **`MergedManifestTest`** (JVM, `make mobile-android-test`) reads the variant's
  `MERGED_MANIFEST` artefact (`processDebugManifest` output). It is a declared, lazy task
  input, so `testDebugUnitTest` depends on the merge and re-runs when it changes. It
  fails on any `<uses-permission>` outside the four, any `<uses-permission-sdk-23>`, any
  `<permission>` / `<permission-tree>` / `<permission-group>` declaration, on
  `allowBackup` other than `false`, on a missing `dataExtractionRules`, on extraction
  rules that do not exclude every domain from both cloud backup and device transfer, and
  on a package other than `mt.taptime.app`.
- **Measured, 2026-11-18:** the merged manifest and the APK (`aapt2 dump permissions
  app-debug.apk`) carry exactly the four.
- **`androidx.core` adds one, measured.** Without the two `tools:node="remove"` lines in
  the source manifest, the merge gains `<permission>` and `<uses-permission>` for
  `mt.taptime.app.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` (an app-private signature
  permission for ContextCompat's not-exported receivers on API < 33). This app
  registers no broadcast receiver, so the pair is removed by name.
- **Mutation record** (each applied, `testDebugUnitTest` run, source restored, green
  again): remove that `tools:node="remove"` pair → 2 tests red · add `CAMERA` → red ·
  `allowBackup="true"` → red · drop one domain from `<device-transfer>` → red.

Components the libraries merge in (not permissions, recorded so a reader is not
surprised): `androidx.startup.InitializationProvider` (not exported; lifecycle process and
profileinstaller initialisers, emoji2's removed — see *Fixed after review*) and `androidx.profileinstaller.ProfileInstallReceiver`
(exported, guarded by `android:permission="android.permission.DUMP"` — a requirement on
the *sender*, which only the shell/system holds; the app does not ask for DUMP).

## No backup, no transfer

`android:allowBackup="false"` stops cloud backup. On Android 12+ it does **not** stop a
device-to-device transfer, so `android:dataExtractionRules="@xml/data_extraction_rules"`
excludes every domain (`root`, `file`, `database`, `sharedpref`, `external` and the
four `device_*` ones) from both `<cloud-backup>` and `<device-transfer>`. The device key
of M16-12 is non-exportable in any case; the rule is that the rest of the app data does
not travel either (card M16-11).

## Red-line scan

`scripts/redline-check.sh` scans `mobile/` (it is in `SRC` and `B1_SRC` once the
directory exists; the "mobile/ yok" line no longer prints). The build output under
`app/build/` is not scanned: ripgrep honours this directory's `.gitignore`.

**S2 (added by this card, hand-over of M16-02's last check).** ripgrep ignores what
`.gitignore` ignores and never looks at the git index, so a file tracked with
`git add -f` under an ignore pattern (`mobile/.idea/Leak.kt`, a file under this build's
`/build/`) would be scanned by no rule. S2 fails when `git ls-files -ci --exclude-standard`
is not empty — over the whole tree, not only `mobile android`, because the same gap
existed for every scanned root (measured 2026-11-18: 0 hits). Pin:
`cmd/tappa/scripttest` `TestRedline_M16_11_TrackedButIgnoredIsNotScanned`.

**B1 on the real tree, measured 2026-11-18** (probes added, scan run, probes removed): a
`"Tappa"` literal in `MainActivity.kt` and a `<string>Tappa</string>` in
`res/values/strings.xml` both FAIL with their line; the same word in an XML `<!-- -->`
comment (`colors.xml`) and in a `src/test` Kotlin literal does not — the two path-bound
exemptions M16-02 wrote for the Gradle layout hold on the real one. S2 on the real tree:
a force-added `app/build/outputs/apk/debug/output-metadata.json` FAILs by name.

**M16-02's known false positives, re-measured on the real tree:** the scaffold has no
HTTP client, no log call and no location call, so none of them has anything to fire
on yet (`location` as an HTTP header name, a bare `fix`/`loc`/`code` argument, `$code`
in a string). M16-12/M16-13 measure them when that code exists. No `tools:node="remove"`
line for a location permission was needed (no library brings one in), so R2 has no
waiver to carry.

## Observed, not fixed

- **Mobile CI** — done in M16-12: see *CI* under *Activation* below.
- **`android.hardware.location` is an implied required feature** (review finding B-3).
  Because the manifest asks for the location permissions, Google Play treats
  `android.hardware.location` as a required feature unless a `<uses-feature>` says
  otherwise, which would hide the app from devices without one. Whether to declare it with
  `android:required="false"` is decided in M16-13 or M16-27; no code change here.

## Fixed after review

- **emoji2 no longer asks a font provider at start** (review finding B-2, ADR 0045 §2.2
  "no third-party SDK"). `EmojiCompatInitializer` (emoji2, through Compose `ui`) asked the
  system font provider (on most phones Google Play services) for `emojicompat-emoji-font`
  at start, and that provider may download it from Google. The source manifest now removes
  that one `<meta-data>` from `androidx.startup.InitializationProvider` (`tools:node="merge"`
  on the provider, `tools:node="remove"` on the entry); the merged manifest keeps
  `ProcessLifecycleInitializer` and `ProfileInstallerInitializer`.
  `MergedManifestTest.theEmojiFontProviderIsNotAskedAtStart` fails if any `androidx.emoji2`
  meta-data comes back, with `ProcessLifecycleInitializer` as its control. Mutation
  (2026-11-18): the removal block deleted → that test red; restored → green.
  **Cost:** emoji newer than the phone's own system font may show as empty boxes on older
  phones, because no newer emoji font is fetched.

## Activation (M16-12)

ADR 0026's two steps for a device key, over M16-07's wire contract
([`docs/plan/m16-mobil.md`](../../docs/plan/m16-mobil.md), *"Tel sözleşmesi"*):

```
code ─▶ lookup ─▶ 1 Welcome ──"Agree and continue"──▶ bind ─▶ 2 Get ready ─▶ 3 Tap
                   (+ terms sheet)                  (new key)                  │
                                     first genuine NFC read of an employer's plaque
                                                                               ▼
                                               complete (signed by the bound key) ─▶ Activation complete
```

| Step | Request | What the app does |
|---|---|---|
| code | — | Q40: the invitation link opens the browser, not the app. The person pastes the link (or the code); `InviteCode` reads the 43-character code out of `?code=`. Held in memory only. |
| lookup | `POST /app/v1/activate/lookup` (unsigned) | the consent screen's facts: employer, person, venue, Wi-Fi, brand. Spends nothing. |
| 1 → 2 | `POST /app/v1/activate/bind` (unsigned) | a **new** Keystore key (every activation binds a fresh pair, M16-07); its raw point as `publicKey`, `consent: true`. The server answers an activation id and a challenge. Nothing is spent, no device exists yet. |
| 3 | `POST /app/v1/activate/complete` | NFC reader mode is on **only** while step 3 is on screen and in the foreground. The plaque's URL must be this app's `<base>/t` (`PlaqueRead`), its query goes as `sun`; the request is signed in the nonce-signed format with `App-Device-Id` = activation id, `App-Nonce` = challenge. The server runs `CompleteByTap`'s order (`internal/handler/appactivate.go`); the app judges nothing about the chip. |

Outcomes of complete, mapped by `ActivationFlow`: `activation_tap_failed` / `activation_needs_touch`
/ `rate_limited` / a 5xx / no answer → stay on step 3 and tap again (the same bind);
`new_key_required` → drop the key, generate a new pair, bind again with the consent already
given, tap again; `activation_not_ready` (the binding moved) → back to step 1 with the same
code; `invalid_invite` → back to the code screen. No `transactions` row is written by any of
it (CLAUDE.md §5).

### The key

- **P-256 signing key in `AndroidKeyStore`** (`device/KeystoreKeys.kt`), `PURPOSE_SIGN`,
  `SHA256withECDSA`, signatures DER, sent as unpadded base64url. The private half cannot be
  exported (the Keystore hands out a handle; `getEncoded()` is null), and the app's data does
  not travel (M16-11's backup rules).
- **StrongBox when the phone has one** (API 28+, `FEATURE_STRONGBOX_KEYSTORE`); a StrongBox
  generation that throws (`StrongBoxUnavailableException` is a `ProviderException`) falls back
  to the TEE once.
- **Where it really lives is read back, not assumed**: `KeyInfo.securityLevel` on API 31+
  (`strongbox` / `tee` / `secure_hardware` for *unknown secure* / `software`); on 26–30 only
  *inside secure hardware* is reported, so a key generated with the StrongBox flag is recorded
  `strongbox`, any other secure one `secure_hardware`, and a software one `software`. The value
  is **recorded with the device** in the app's private preferences (`key_backing`,
  `device/PrefsDeviceState.kt`) and **not sent** — the wire contract has no field for it and the
  server decodes strictly. A software-backed key is not refused (emulators, some old phones);
  attestation (M16-25) is where the server learns this.
- **No user-authentication requirement on the key** (CLAUDE.md §4.1, ADR 0045 §2.3): the phone's
  own screen lock is not Taptime's layer. `scripts/redline-check.sh` R1 fails on the Keystore
  builder method that would add one anywhere under `mobile/` — so the rule is enforced by the
  scan, and neither this file nor the code names that method.
- The SPKI the Keystore returns is cut to the raw 65-byte point by a **fixed 26-byte header
  check** (`wire/PublicPoint.kt`), pinned by the vectors' `spki` entry.

### The signed requests and the clock

`wire/Canonical.kt` builds the two canonical texts (`tappa-app-request-v1`, `tappa-app-read-v1`)
and refuses every field the server would refuse. `wire/AppClient.kt` is the one client; nothing
in it logs. The clock-skew contract (card M16-12): every reply's `Date` re-measures the
server-minus-phone offset (`wire/ServerClock.kt`) and signed reads carry the corrected second;
a 401 to a signed read or a nonce-signed request is retried **once** (the 401's own `Date` is
the fresh measurement; a nonce-signed retry fetches a fresh nonce) and only the second 401 means
signed out. On launch the app asks `GET /app/v1/me`; a signed-out answer drops the stored device
and its key and shows the code screen with *"This phone was signed out"*. No answer at all keeps
the device (offline is not signed out).

Where the app keeps what: the device id, the key's alias, the backing and the person's name in
private preferences (no secret among them); the invite code, the activation id and the
challenge **in memory only** — the activity declares `configChanges` so a rotation does not
rebuild it and nothing goes into a saved-state Bundle.

`BuildConfig.BASE_URL` is `https://taptime.mt`; a debug build may point elsewhere with
`gradle -Ptaptime.baseUrl=https://…` (the build refuses anything but `https://`; no cleartext
exception is declared — a local server for M16-14 needs TLS or that exception, decided there).

### JSON

Request bodies are written by hand (`wire/Json.kt`, `JsonObjectWriter`): flat, in the contract
table's order, byte-exact, so the signed bytes are known. Replies are decoded on the phone with
the platform's `org.json` (`wire/OrgJsonDecoder.kt`), converted to plain maps; every mapping to
the app's types is in `wire/Wire.kt`. **Measured:** `org.json` on the unit-test classpath is
android.jar's stub — `OrgJsonDecoder.decodeObject("{}")` on the JVM throws *"Method nextValue in
org.json.JSONTokener not mocked"*. No JSON library was added (Q36); the tests use a test-only
RFC 8259 reader (`src/test/.../MiniJson.kt`) that yields the same map shape. The org.json
adapter itself is therefore not run on the JVM; it is the device tour's (M16-14).

### Tests (`make mobile-android-test`)

| Class | What it holds |
|---|---|
| `VectorsTest` | every request and read text of `test/fixtures/appdevice_vectors.json` byte for byte (texts, body digests, text digests); every OpenSSL signature verifies **exactly** when the vector says `valid` (5 of 8 request, 3 of 6 read); the 12 malformed requests are refused by the builder; the SPKI vector is key A; a JDK key signs what the format verifies |
| `ContractPinTest` | route paths, header names, canonical prefixes, the 16-code error vocabulary **with statuses**, the `android` platform and the request bodies' field names, read from the Go source |
| `WireTest` | the request bodies byte for byte; the writer's escaping; the contract's reply shapes; refusals of missing / mistyped fields |
| `ClockSkewTest` | the `Date` offset; a skewed read retried once with the server's second; a second 401 is signed out with no third try; refusals and no-answer are not retried; a nonce-signed retry uses a fresh nonce |
| `InputsTest` | the code from a link or alone; only `<base>/t` yields a SUN query; the accent's label and edge equal ADR 0023 §3's table |
| `ActivationFlowTest` | the whole activation over a socket against `FakeAppServer` (`com.sun.net.httpserver`, the JDK's — usable on the unit-test JVM, measured): request order, the bound key is the one activated, replay → tap again, no counter → not a touch, `new_key_required` → fresh pair, a moved binding → back to consent with no counter spent, a phone **five minutes slow** still set up after one corrected retry, a signed-out phone drops device and key |
| `MergedManifestTest` | M16-11's, unchanged |

The fake server builds the canonical text **itself** (string concatenation from the format's
description), so the app's builder is not checked against its own output; the JDK writes `Date`
on every answer, as Go does. Mutations (2026-11-18, each applied, tests run, restored): read
prefix `v1`→`v2` → 4 red · the signed read's retry removed → 4 red · `Date` not observed → 3 red
· device id case check removed → 2 red.

Kotlin test names never start with `Test` (the repository's citation gate,
`cmd/tappa` `TestEveryNamedTestExists`, reads every `Test…` word as a Go test name).

### CI

`.github/workflows/ci.yml` job **`android`** (Q36 (d); M16-11 review B-1): JDK 21 from
`actions/setup-java`, Gradle 9.7.1 downloaded from services.gradle.org and checked against its
published SHA-256 (`acd53f1e…f804d20a`), the runner image's Android SDK measured and
`platforms;android-36` / `build-tools;36.0.0` installed only if missing, then
`testDebugUnitTest` for this app **and** for the relay (`android/`). First-party actions only.
The job has no `timeout-minutes` key (`cmd/tappa`'s CI-timeout test allows exactly one, the
`check` job's); each Gradle run is bounded by `timeout 30m` instead. A red `android` job makes the
`ci` workflow red, and `deploy.yml` deploys `main` only on a green `ci`.

### Texts — the browser wizard's, and the differences (§9 questions)

The screens follow the wizard's order and words (`web/templates/pages/activate.templ`, the
problem texts of `internal/handler/activate.go`), all in `res/values/strings.xml`. What differs:

1. **APP-ONLY code screen** — *"Setup"*, *"Paste your activation link"*, *"Copy the personal link
   your workplace sent you and paste it here."*, *"Continue"* (Q40: the link is not the app's).
2. **ADAPTED, step 1 panel and terms** — the browser's closing promise of what is never
   collected names a noun that is an R1 trigger, and R1 has no waiver under `mobile/` (the
   pre-filter that lets the browser's sentence through is not applied there, M16-02 audit). It
   reads *"No finger scans, no face, no voice."* in both places, word for word as the iOS app
   (`mobile/ios/Taptime/ActivationCopy.swift`; coordinator decision, 2026-11-18): the §4.1
   promise stays on the screen.
3. **ADAPTED, terms *"What is recorded"*** — *"A cookie keeps this phone signed in…"* becomes
   *"A key kept on this phone keeps it signed in, so you don't need a password."* (the app has
   no cookie, ADR 0045 §2.3).
4. **ADAPTED, step 2** — *"Two quick things before you tap."* becomes *"One quick thing before you
   tap."*; the second thing, *"Stay in this browser"*, is about the browser's cookie and has no
   app twin.
5. **Dropped from step 3** — the *"Tapped and saw “Finish setup in this browser”?"* note (the app
   reads the plaque itself in reader mode).
6. **APP-ONLY** — *"Turn on NFC in your phone's settings, then tap the plaque."*, *"This phone
   can't read the plaque, so it can't be set up in this app."*, and the no-answer problem
   *"Couldn't reach Taptime"* / *"Check your connection and try again."*.
7. **Not shown** — the browser's *"This phone belongs to someone else"* warnings (a browser
   session held by another person has no app twin: the app activates only when it holds no
   device); the logo (no logo bytes reach the app yet — M16-07 hands over only digest and box;
   the header is the wordmark, ink beside an accent); the waiting screen's moving rings (the
   plaque is drawn still).
8. **Kept verbatim although browser-worded** — *"You can close this page."* on the two
   *"Activated"* screens.
