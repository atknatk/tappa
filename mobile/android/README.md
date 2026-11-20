# Taptime staff app — Android (M16-11 scaffold, M16-12 activation, M16-13 tap)

The optional native staff app decided in
[ADR 0045](../../docs/adr/0045-istege-bagli-native-uygulama.md), Android half. M16-11 was
the **scaffold** (the closed permission list, no backup, the build and test targets);
M16-12 adds the **device key and the activation**, M16-13 the **plaque tap** (both below). The
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
serve a one-shot, foreground reading at the press, M16-13; they are requested at the press,
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
- **`android.hardware.location` was an implied required feature** (review finding B-3) —
  decided and fixed in M16-13, see *The tap* below.

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
| code | — | Q40: the invitation link opens the browser, not the app. The person pastes the link (or the code); `InviteCode` reads the 43-character code out of `?code=` — only of **this server's** activation link (the base URL's scheme and host, path exactly `/activate`; a look-alike host's code is never sent). Held in memory only. |
| lookup | `POST /app/v1/activate/lookup` (unsigned) | the consent screen's facts: employer, person, venue, Wi-Fi, brand. Spends nothing. |
| 1 → 2 | `POST /app/v1/activate/bind` (unsigned) | a **new** Keystore key (every activation binds a fresh pair, M16-07); its raw point as `publicKey`, `consent: true`. The server answers an activation id and a challenge. Nothing is spent, no device exists yet. |
| 3 | `POST /app/v1/activate/complete` | NFC reader mode is on **only** while step 3 is on screen and in the foreground. The plaque's URL must be this app's `<base>/t` (`PlaqueRead`), its query goes as `sun`; the request is signed in the nonce-signed format with `App-Device-Id` = activation id, `App-Nonce` = challenge. The server runs `CompleteByTap`'s order (`internal/handler/appactivate.go`); the app judges nothing about the chip. |

Outcomes of complete, mapped by `ActivationFlow` to the iOS twin's destinations:
`activation_tap_failed` / `unknown_plaque` / `foreign_tenant` / `stale_context` /
`activation_needs_touch` / `rate_limited` / no answer → stay on step 3 and tap again (the same
bind); `new_key_required` → drop the key, generate a new pair, bind again with the consent
already given, tap again; `activation_not_ready` (the binding moved), a server error or any
other refusal → back to step 1 with the same code (the key dropped); `invalid_invite` → back to
the code screen. A phone without an NFC reader is refused at launch, before the code screen.
No `transactions` row is written by any of
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
  server decodes strictly.
- **A software-backed key is refused** (`KeyGate`, coordinator decision 2026-11-18 — the iOS twin
  refuses a phone without a Secure Enclave): when the level read back is `software`, the key is
  deleted, no bind is sent, and the phone shows *"This phone can't be set up"*. Every other level
  passes, `unknown` included (a level this version cannot name is not evidence of software). The
  gate is a pure function of the level the generator reports, driven with every value in
  `ActivationFlowTest`.
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
server-minus-phone offset (`wire/ServerClock.kt`) and signed reads carry the corrected second — unless the `Date` is more than **24 hours** away from the phone's clock, which is ignored and the previous offset kept (`Date` is unsigned; one forged answer must not move every later signed second by days);
a 401 to a signed read or a nonce-signed request is retried **once** (the 401's own `Date` is
the fresh measurement; a nonce-signed retry fetches a fresh nonce) and only the second 401 means
signed out. On launch the app asks `GET /app/v1/me`; a signed-out answer shows the code screen with
*"This phone was signed out"* and **keeps the stored device and its key** (security review,
2026-11-18, same rule as iOS): a 401 is also what a server-side fault — an RLS or GRANT
regression that makes the resolver miss every device — answers to every phone at once, and a
deleted Keystore key cannot come back. Only a new successful activation replaces the record (and
then deletes the old key). A record whose Keystore entry is already gone is dropped — that state
is permanent. No answer at all keeps the device (offline is not signed out). Mutation
(2026-11-18): the deletion on 401 put back → `aSignedOutPhoneKeepsItsRecordAndKeyUntilANewActivation` red.

Where the app keeps what: the device id, the key's alias, the backing and the person's name in
private preferences (no secret among them); the invite code, the activation id and the
challenge **in memory only** — the activity declares `configChanges` so a rotation does not
rebuild it and nothing goes into a saved-state Bundle.

`BuildConfig.BASE_URL` is `https://taptime.mt`. **Release is pinned** to it: with
`taptime.baseUrl` set, any release task stops the build. A **debug** build may point elsewhere
with `gradle -Ptaptime.baseUrl=https://…` (anything but `https://` is refused); the value is
written as an escaped Java string literal. Measured 2026-11-18: `-Ptaptime.baseUrl=https://staging.example
assembleRelease` → BUILD FAILED (*"the release BASE_URL is fixed to https://taptime.mt"*);
`assembleRelease` → release `BuildConfig.BASE_URL = "https://taptime.mt"`; debug with
`https://staging.example/a"b` → `"https://staging.example/a\"b"`; debug with `http://x` → refused.
`InputsTest` pins the debug default. Cleartext is off **explicitly**
(`android:usesCleartextTraffic="false"`, no `networkSecurityConfig`; `MergedManifestTest`) — a
local server for M16-14 needs TLS, decided there.

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
| `ClockSkewTest` | the `Date` offset; a `Date` more than a day away ignored; a skewed read retried once with the server's second; a second 401 is signed out with no third try; refusals and no-answer are not retried; a nonce-signed retry uses a fresh nonce |
| `InputsTest` | the code from this server's `/activate` link or alone, never from another host or path; only `<base>/t` yields a SUN query; the accent's label and edge equal ADR 0023 §3's table |
| `ActivationFlowTest` | the whole activation over a socket against `FakeAppServer` (`com.sun.net.httpserver`, the JDK's — usable on the unit-test JVM, measured): request order, the bound key is the one activated, replay → tap again, no counter → not a touch, `new_key_required` → fresh pair, a moved binding → back to consent with no counter spent, a phone **five minutes slow** still set up after one corrected retry, a signed-out phone keeps its record and key until a new activation, a record without its key is dropped, a software key is never bound, a 307 is not followed (`Refused(307)`, the target never asked) |
| `MergedManifestTest` | M16-11's, plus cleartext off explicitly (M16-12) |

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
`check` job's); every step that can hang is bounded on its own instead — the Gradle download by
`curl --max-time 300`, `sdkmanager` by `timeout 5m` / `timeout 15m`, each Gradle run by
`timeout 30m`. A red `android` job makes the
`ci` workflow red, and `deploy.yml` deploys `main` only on a green `ci`.

### Texts — the browser wizard's, one list with iOS

The screens follow the wizard's order and words (`web/templates/pages/activate.templ`, the
problem texts of `internal/handler/activate.go`), all in `res/values/strings.xml`. **The rule
(coordinator, 2026-11-18): every line that means the same thing on both platforms is the same,
character for character, as the iOS app's `mobile/ios/Taptime/ActivationCopy.swift`** (compared
by script against its commit `ef82bc3d`, 2026-11-18: every string here is an iOS literal except
the ones listed under *Android only* and *platform word*). What differs from the browser:

1. **App-only code screen** (iOS's words) — *"Setup"*, *"Set up this phone"*, *"Paste the
   activation link your workplace sent you."*, the field label *"Activation link"*,
   *"Continue"*, and inline under the field when the paste is not this server's link or a code:
   *"That isn't an activation link. Copy the whole link from the message your workplace sent and
   paste it here."* (Q40: the link is not the app's.)
2. **R1** — the browser's closing promise of what is never collected names a noun that is an R1
   trigger, and R1 has no waiver under `mobile/`. It reads *"No finger scans, no face, no
   voice."* in the panel and the terms, as on iOS; the user kept it (2026-11-18).
3. **Adapted, terms *"What is recorded"*** — *"A key kept on this phone keeps it signed in, so you
   don't need a password."* instead of the browser's cookie sentence.
4. **Adapted, step 2** — *"One quick thing before you tap."*; the browser's *"Stay in this
   browser"* has no app twin.
5. **Adapted, problem hints** — *"Wait a little and try again."* (too many), *"Try again in a
   minute."* (server), *"Go through the steps again, then tap the plaque."* (not ready): the
   browser's *"open your link again"* would send the person to the browser (Q40). The activated
   screens say *"You can close the app."*
6. **App-only** — *"Can't reach Taptime"* / *"Check your connection and try again."* (no answer).
7. **Platform word** — the two refusals iOS writes with *"iPhone"* say *"phone"* here:
   *"This phone can't read the plaque"* / *"Setting up the app needs a tap on the plaque, and this
   phone has no NFC reader."* / *"Ask your manager — setup without a plaque tap isn't available
   yet."* (no NFC reader, refused **at launch, before the code screen**, as on iOS) and *"This
   phone can't be set up"* / *"The app keeps its key in this phone's secure chip, and the chip
   isn't available."* / *"Ask your manager — you can still use Taptime in your browser."*
   (software key store, `KeyGate`).
8. **Android only** — *"Turn on NFC in your phone's settings, then tap the plaque."* (the NFC
   switch is off; iOS has none to name). iOS's *"Scan the plaque"* button and its scan sheet
   have no Android twin: Android reads the plaque in reader mode while step 3 is on screen.
9. **Problems are inline** — iOS shows a problem on its own screen with *"Try again"* / *"Start
   again"*; Android shows the same words as a notice on the screen the person is sent back to.
   Where each answer leads is iOS's: a plaque-judged refusal (tap failed, unknown or foreign
   plaque, stale context), too many and no answer → the tap step; a server error or any other
   refusal → the consent step (the key dropped, a new one bound on *"Agree and continue"*);
   *not ready* → the consent step; an unusable invitation → the code screen.
10. **Dropped / not shown** — the step-3 note *"Tapped and saw “Finish setup in this browser”?"*;
    the browser's *"This phone belongs to someone else"* warnings (the app activates only when it
    holds no live device); the logo (no logo bytes reach the app — M16-07 hands over only
    digest and box, and M16-13 left it so: see *The tap*; the header is the wordmark, ink beside
    an accent); the waiting screen's moving rings (the plaque is drawn still).

## The tap (M16-13)

GET /t and POST /api/checkin's twin over M16-07's wire contract. The server decides everything
(`checkin.Service.Record`, `client='app'` server-derived); the app judges nothing about the chip.

```
plaque URL ─▶ tap/start (signed read) ─▶ Tap screen ──press──▶ one reading ─▶ /nonce ─▶ /tap ─▶ Result
 (NFC or App Link)   nothing written,        name, venue,      (TapLocation,     nonce-signed       no button
                     no counter advanced     one button        ≤ 8 s)
```

### How a plaque opens the app

Two filters on `MainActivity`, both `https://<BASE_URL host>/t` and nothing else
(`MergedManifestTest.thePlaqueRouteOpensTheAppAndNothingElseDoes`; the host is a manifest
placeholder from `BASE_URL`, so a debug build pointed elsewhere takes that host's plaques):

- **`NDEF_DISCOVERED`** — the NFC read of the plaque's URI record. Whether Android's tag
  dispatch picks the app before the browser is **to be measured on a real phone** (M16-14).
- **`VIEW` + `BROWSABLE`, `autoVerify="true"`** — the same URL as a link: a QR scanned by the
  camera, a link in a message. Verified against `/.well-known/assetlinks.json` (M16-08).
  `/activate` is not the app's (Q40). M16-22 adds a third filter, VIEW only, for a venue screen's
  live QR (`/q`) — see *QR codes* below.

The URL reaches `TapFlow.start` (or, during activation step 2/3, the activation's complete — the
browser's consented activation takes `GET /t` the same way). `PlaqueRead` re-checks scheme, host,
port and the exact path before anything is sent. **A plaque URL is acted on once**, when it is delivered:
reopened from Recents (`FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY`) or after the system rebuilt the
activity, the same intent carries the same spent counter, and a press on it could only record a
`sun-invalid` reject that starts the 60-second debounce over the real next tap (`tap.js` keeps a
restored NFC page as it was for the same reason) — such a launch is the launcher's. A new touch
while a press is in flight is ignored (its counter is simply the next one); a new touch on the
tap, result or problem screen starts over.

**One press per tap screen** (security review 2026-11-18, `PressGuard`): the primary button and
*"Not right? …"* are drawn from one composition, and two clicks in the same frame both carry its
state. The press judges the **live** screen on the main thread — the same tap screen, not busy — and
marks it busy before anything else, so the second click of the pair does nothing. (Judged by the
captured state, both went through: two POSTs with one context, the second recorded as a
`sun-invalid` reject and shown as *"Not counted"*.) `PressGuardTest`.

**"Open in browser" sends a rebuilt URL** — `<base>/t?` + the query `PlaqueRead` verified
(`PlaqueRead.browserUrl`, the iOS twin's rule), never the URL as the intent carried it.

**No secret in a printed value** (security review 2026-11-18): every data class carrying the plaque
URL, the SUN query, the signed context, a nonce, the activation challenge or the invite code
overrides `toString` (`<redacted>`) — a generated one would print them into any log or crash line
it reached. `RedactionTest` holds it over the source (every such data class under `src/main`) and
over the values.

### Screens — the browser's order and words

| Screen | What is on it | Browser source |
|---|---|---|
| Tap | header (wordmark; ink beside an accent) · docket *"Tapping"* / *"Hello Maria"* / the tapped venue · **one button**: *"Clock in"*, *"Clock out"* or *"Tap"*, in the tenant accent · under a predicted direction only, *"Not right? Clock out"* (ink, underlined, no accent). While the press is under way both are disabled and the button says *"Tapping…"*. | `tap.templ`, `view.go`, `tap.js` |
| Result | header (wordmark, **no accent**) · docket: venue, headline (*"Clocked in"* / *"Clocked out"* / *"Tapped"* / *"Already tapped"* / *"Not counted"*), the time in mono, the practice line · closing: on `ok` the brand line and *"All done — you can close the app."*; on `flag` *"You can close the app."*; on `ignored` / `reject` the browser's sentence and *"You can close the app."* **No button** (§9). | `result.templ` |
| Tap problem | the browser's tap problem texts in a notice, with the iOS twin's buttons (`Buttons.of`, below) | `tap.go` `tapProblem*` |
| Not set up | its own problem screen, as iOS's `NotSetUpScreen`: *"This app isn't set up yet"* + **"Open in browser"** + *"Set up this phone"* (the code screen) | adapted from `activate.go`'s *"Finish setup in this browser"* |
| Signed out | the problem screen *"This phone was signed out"* + **"Open in browser"** + *"Start again"* (activation) | `activate.go` |
| Check your phone's clock | a 401 while the last answer's `Date` was a day or more off this phone's clock (`ServerClock.lastAnswerWasFarOff`), at launch, at the start or at the press, instead of *signed out* (M16-16 hand-over (4), iOS's clock screen) + *"Try again"* | app-only |

**The person's name is on the tap screen before anything is recorded** (M16-12 hand-over (5),
user decision 2026-11-18): it is the mitigation of ADR 0045's accepted shared-phone risk —
*"Hello Maria"*, the browser's own greeting.

`TapCopyTest` reads the browser's source (`tap.go`, `tap.templ`, `result.templ`, `view.go`,
`tap.js`, all declared task inputs) and holds every tap string to it; each string is *verbatim*,
*adapted* or *app-only*, and every one is classified.

**One list with iOS** (coordinator decision 2026-11-18): every tap line that has an iOS twin is,
character for character, a literal of `mobile/ios/Taptime/TapCopy.swift` / `ActivationCopy.swift`
(*"iPhone"* read as *"phone"*). `IosCopyTest` holds it; on a branch without the Swift files it is
**skipped**, not passed, and `-Ptaptime.iosRoot=<dir>` points it at another checkout. Measured
2026-11-18 against the iOS commit `543e1acd`: **63 of 63** shared lines identical, *"Not right? …"*
(built by `+` on iOS) identical by its parts.

Differences from the browser:

1. **Adapted — "page" has no app twin:** *"All done — you can close the app."* / *"You can close
   the app."* (browser: *"… close this page."*, the activation screens already say *"close the
   app"*); *"Taptime opens when you hold your phone to the plaque at the door."* (*"This page
   opens …"*); *"Nothing was recorded. Tap screens are only good for a few minutes."* (*"Tap
   pages …"*). The iOS twin's drafts (M16-17, read 2026-11-18) carry the same words.
2. **App-only — no answer from the network** (the browser shows its own offline page):
   - at `tap/start`: *"Can't reach Taptime"* / *"Nothing was recorded."* / *"Check your
     connection and tap the plaque again."* + *"Try again"* + *"Open in browser"*;
   - at the **press**: *"Can't reach Taptime"* / *"We couldn't confirm this tap, so it may not
     have been recorded."* / *"Check your connection and tap the plaque again. Tell your manager
     if that looks wrong."* + *"Try again"* that **resends nothing** (below), and **no** *"Open in
     browser"*.
3. **App-only — not set up:** *"This app isn't set up yet"* / *"This app hasn't been set up for
   Taptime yet, so it can't record this tap."* / *"Open the tap in your browser instead, or set up
   the app with the link your workplace sent you."*
4. **App-only — the phone's clock:** *"Check your phone's clock"* / *"Your phone's date and time
   are too far off for Taptime to accept them. Nothing was recorded."* / *"Set the date and time to
   automatic in Settings, then try again."*
5. **App-only buttons** (the iOS twin's, `Buttons.of`): *"Open in browser"* only where the plaque
   URL is certainly unspent **and** the app cannot go on — not set up, signed out, no answer
   **before** the press (coordinator decisions 2026-11-18, narrowing the user's "error screens
   only"); *"Try again"*, *"Start again"* (signed out → activation) and *"Set up this phone"* (not
   set up → the code screen) — the user-approved app-only buttons.
6. **Not carried:** the logo and the M15-17 claim card — both wait for the separate `/app/v1`
   wire-contract card the coordinator is opening (below); the browser's back-forward-cache reload
   of a QR page (the app has no page cache: a new scan is a new intent).
7. **No *"The plaque wasn't read"*** (iOS's `notRead`). On iOS it is the Core NFC scan sheet
   closing on an error. Android has no app-owned read on the tap path: the system's tag dispatch
   delivers an intent only after it has parsed an NDEF URI record that matches `https://<host>/t`,
   so a failed or foreign read never reaches the app (the system handles it, or nothing happens).
   An intent that matched the filter but is not this server's exact plaque URL (a fragment, another
   port) is *"That link didn't work"* — iOS's `badURL`, the same text. The activation step's reader
   mode is the one app-owned read, and its failure already has M16-12's *"That tap didn't finish
   setup"*.

**"Try again" rules.** Before the press it starts over with the same URL (`Retry.RESTART`: a
server error, no answer, a Keystore that would not open, the clock) — `tap/start` writes nothing
and advances nothing, and a refused signature records nothing. **After a press whose answer was
lost it goes home** (`Retry.HOME`, the launch check) **and resends nothing**: the same signed context
again would carry the same counter, and if the first press did reach the record the second is a
recorded `sun-invalid` reject. The next touch of the plaque is a fresh counter. A press refused with
an answer (expired, another employer's plaque, unknown plaque, too many, server) has no *"Try
again"*: a fresh context comes from touching the plaque.

### No queue — the known cost

There is no offline queue (ADR 0045 §2.8, coordinator decision 2026-11-18). A press whose answer
is lost says *"tap the plaque again"*; the record may or may not exist. If it does, the next touch
within 60 s is `ignored` (recorded, harmless); if it does not, the next touch records. The lost
touch's counter is spent either way — the plaque's counter gap (A1's `ctrGap`) counts it. That
screen therefore offers **no "Open in browser"** (coordinator decision 2026-11-18, as on iOS): if the
lost press did reach the record, the same URL in the browser would record a `sun-invalid` reject and
start the person's 60-second debounce.

### The one reading (`tap/TapLocation.kt`) — CLAUDE.md §4.2

- **Only at the press**, after the button is pressed, never at start or on screen. The permission
  (fine + coarse) is asked **at the press** the first time; a refusal sends no fix and the press
  goes on (§5 rows 6–7; the record is written either way, §4.6).
- **Platform `LocationManager`**, no Play services (Q36). Every ask goes through **one function**,
  `TapLocation.ask()`, which holds the app's only two location requests:
  - **first ask, API 30+:** `getCurrentLocation(provider, CancellationSignal, mainExecutor, …)` —
    one answer, cancellable;
  - **first ask, API 26–29:** `requestSingleUpdate(provider, listener, mainLooper)` (all four
    listener callbacks are written: they are abstract below API 30);
  - **the fresh ask:** `requestSingleUpdate` on every level — a new one-shot registration is
    answered by a new reading, where a second `getCurrentLocation` would hand back the same cache
    within its own window (deprecated on 30+, not removed; **to be measured on a real phone,
    M16-14**).
- **Providers** (`FixRules.providers`): API 31+ the platform's **fused** provider alone (AOSP: it
  combines GPS and network itself, works with the coarse permission); otherwise, with the fine
  permission, **GPS and network side by side** (review D-2: indoors GPS may not fix within 8 s);
  with the coarse permission only, network; otherwise none.
- **The race** (`FixRace`, pure, `FixRulesTest`): each provider asked **once** at the press; the
  first accepted reading wins and every other ask is cancelled; an empty answer waits for the rest;
  all empty is no fix. A **refused cache** (below) gets **one** fresh ask of the same provider in
  what is left of the deadline (review O-1, coordinator decision (a)) — one in all, never another.
  The deadline cancels everything that is out and the press goes without a fix (the record is
  written either way).
- **Deadline 8 s** — `tap.js`'s `FIX_TIMEOUT_MS`, read from `tap.js` by `FixRulesTest`. The timer is
  `tap/FixDeadline.kt` (one `postDelayed`, never re-armed), **apart from** the location file: the
  red-line scan's R2 periodic net fails a file holding both (card *Tuzaklar*).
- **A reading taken before the request is refused** (`FixRules.accept` / `isStale`, by
  `elapsedRealtimeNanos`): `tap.js` asks with `maximumAge: 0`, and `getCurrentLocation` may answer
  with a cached one — which is why the race asks once more. Out of range or not finite is no
  reading.
- **Mock flag** — `Location.isMock()` on API 31+, `isFromMockProvider()` before, of **that**
  reading; sent as `"mockLocation":true` **only when true and only with a fix** (`Bodies.tap`).
  False is never written: its absence proves nothing (ADR 0045 §2.4).
- **`LocationCallSiteTest`** pins the call sites over the whole of `src/main`: `getCurrentLocation`
  and `requestSingleUpdate` once each, in `TapLocation.kt` only, both inside `ask()`; `ask()` called
  from two places only (the first asks, iterating the providers once, and FixRace's one fresh ask),
  and no other loop in the file; no last-known or continuous request anywhere; `TapLocation` constructed once and read only inside `MainActivity.withOneFix`,
  which only the press calls; no timer in `TapLocation.kt`, the timer in `FixDeadline.kt`, which
  asks for no position.
- **Nothing is logged** — `NoLogTest` pins the inventory of log calls under `src/main` at **zero**
  (and `android.util.Log` is not imported); a future log line is held to carry no coordinate,
  signature, nonce, context, SUN query or code.

### B-3 — location hardware is optional (decided here)

`<uses-feature android:name="android.hardware.location" android:required="false"/>`: a fix is
backup evidence, never a requirement (§5 rows 6–7), so a phone without location hardware must
still see the app (an NFC-less phone already does, ADR 0045). Measured on the debug APK
(2026-11-18, `aapt2 dump badging`, build-tools 36.0.0): `uses-feature-not-required:
name='android.hardware.location'`; the only `uses-feature` left is the default `faketouch`.
`MergedManifestTest.noHardwareFeatureIsRequired` pins it.

### No logo, no claim card

Both wait for the separate `/app/v1` wire-contract card the coordinator is opening (2026-11-18):
the logo bytes have no app route (`GET /t/logo/{sha}` answers a browser session, and the
peer-plaque `?tag=` distinction is not in `tap/start`'s `brand`), and the tap reply has no field
for the M15-17 claim card. Until then the header is the wordmark, ink beside an accent (K-2a).

### M16-12 hand-overs done here

1. **API 31+ unknown security level** — `KeyBacking.fromReport`: a level this version cannot name
   is UNKNOWN (accepted) only when `isInsideSecureHardware` also says so; otherwise SOFTWARE
   (refused by `KeyGate`).
2. **`InviteCode` compares the port** as `PlaqueRead` does (`effectivePort`, one function).
3. **A Keystore that will not open the key** — at launch and at the tap — retries once, then shows
   iOS's text *"Taptime couldn't open this phone's key"* / *"Nothing was changed on this phone."* /
   *"Try again in a moment. If it keeps happening, you can set this phone up again."* with *"Try
   again"*. The record and the key are kept (never deleted on a Keystore failure).
4. **A key whose level cannot be read is deleted** before the failure goes up (`KeystoreKeys.generate`).
5. **The name before the tap** — above.

### Tests added (`make mobile-android-test`)

| Class | What it holds |
|---|---|
| `PressGuardTest` | two clicks in one frame send one press; a click drawn for another (or no) tap screen does nothing; identity, not equality |
| `RedactionTest` | every data class carrying a secret overrides `toString`; the overrides print none of it |
| `IosCopyTest` | every shared tap line is a literal of the iOS app's copy (skipped without the Swift files) |
| `TapFlowTest` | the tap over a socket against `FakeAppServer`: the clock screen at the start and at the press (an in-range answer clears it); the problem screens' buttons are the iOS twin's; `tap/start` is a signed read of the plaque's query verbatim, no nonce; the press body byte for byte (want, fix, mock only when true); no fix / no direction send neither; not set up, key gone (dropped), Keystore failing twice (kept, *Try again*) or once (goes through), a throwing signer at start and at the press (nothing sent); another host's URL sent nowhere; signed out after one retry at start and at the press; every refusal of start and press to its screen and retry rule; no answer to start → retry; a lost press answer → *Try again* goes home, sent once |
| `FixRulesTest` | provider names are the platform's; the providers by API level, permission and what is on (GPS **and** network below 31); the race: first fresh reading wins, empty waits, a refused cache gets one fresh ask and no more, the deadline ends it; a reading before the request refused; out of range refused; the deadline is `tap.js`'s |
| `TapCopyTest` | every tap string verbatim / adapted / app-only against the browser's source; every result line has its string; the result's lines verdict by verdict |
| `LocationCallSiteTest` | the call-site allow-list above |
| `NoLogTest` | zero log calls under `src/main` |
| additions | `WireTest` tap bodies and replies · `ContractPinTest` tap bodies against `apptap.go`'s structs · `InputsTest` the code link's port · `ActivationFlowTest` Keystore failing at launch, `fromReport` for every level · `MergedManifestTest` the two plaque filters, no required feature |

**Mutations** (2026-11-18, applied, tests run, restored): mock flag always written → `WireTest`
red · a second `getCurrentLocation` in `MainActivity` → `LocationCallSiteTest` red · a `Log.d` in
`TapFlow` → `NoLogTest` red · the before-the-request check dropped → `FixRulesTest` red · one tap
hint reworded → `TapCopyTest` red · location `required="true"` → `MergedManifestTest` red · *Try
again* offered after a lost press → `TapFlowTest` red · `InviteCode`'s port check dropped →
`InputsTest` red. Review fixes (2026-11-18): `PressGuard` ignoring busy → `PressGuardTest` red · `FixRace`
never asking fresh → `FixRulesTest` red · GPS **or** network instead of both → `FixRulesTest` red ·
`TapStartReply`'s `toString` override dropped → `RedactionTest` red · the browser URL passed on as it
arrived → `InputsTest` red.

## QR codes (M16-22)

No QR reader in the app and **no camera permission** (Q36 (c), 2026-11-14): the phone's own camera
reads the code and the App Link opens the app. Two kinds of code, one tap screen.

| Code | URL (measured on the server) | Filter | tap/start body |
|---|---|---|---|
| **Static** — printed on the plaque | `sun.PlaqueQRURL`: `https://<host>/t?tag=<UID>` (no `ctr`, no `cmac`) — the plaque route itself | the existing `/t` VIEW filter (M16-13); nothing new | `{"sun":"tag=<UID>"}` — the server derives `channel=qr` |
| **Live** — a venue screen's rotating code (M16-20) | `liveqr.TapURL`: `https://<host>/q?t=<token>` (~20 s) | a **new** VIEW + BROWSABLE filter, `autoVerify="true"`, path exactly `/q` (no NDEF: a QR is never an NFC read) | `{"liveQR":"t=<token>"}` — the URL's query as written |

- **`TapLink`** (`activation/Inputs.kt`) is what opened the tap screen: `PlaqueRead` (`/t`) or
  `LiveQrRead` (`/q`), the same origin rule (scheme, host, port, exact path, no user, no
  fragment). Anything else is *"That link didn't work"* and is sent nowhere. The query is never
  printed (`toString` = `<redacted>`).
- **The server opens the token** (`apptap.go` `tapStartLive`, GET /q's own reader
  `liveQRTap.open`): shape and tag in constant time, the screen from its row, the row's venue equal to
  the token's. The app judges nothing. A code that does not verify still draws the tap screen and the
  press records a reject (Q39 (a)); an expired one a flag (Q39 (b)); GPS alone a flag
  (`base:live-qr-requires-ip`). The press is the ordinary `POST /app/v1/tap`.
- **The screens are the plaque's** — the same tap screen (name, venue, one button, *"Not right? …"*),
  the same result and problem screens, **no new text**. *"Open in browser"* rebuilds the `/q` URL
  from the base, under the same rule as the plaque's (only while nothing was pressed).
- **Acted on once**: a live QR intent reopened from Recents is the launcher's, as a plaque's.
- **During activation** a live QR finishes nothing (the plaque reader is the activating read);
  NFC-less activation is M16-23.
- **Phone without NFC**: activation refuses it at launch (*"This phone can't read the plaque"*) —
  unchanged here; a live QR's tap/start needs no NFC.

Tests: `TapFlowTest` (`aLiveQrIsASignedReadOfItsQueryAndTheSamePress`, `…OfAnotherHostOrPath…`,
`…OnAPhoneWithNoDevice…`), `InputsTest` (`onlyThisServersLiveQrRouteYieldsALiveQuery`,
`aTapLinkIsThePlaqueOrTheLiveQrAndNothingElse`), `ContractPinTest`
(`theLiveQrRouteIsTheServersPath` — `/q` and `t` read from `internal/liveqr/token.go` and
`internal/handler/livetap.go`; `theTapBodiesUseTheServersFieldNames` — `sun` ∪ `liveQR`),
`WireTest` (the `liveQR` body), `MergedManifestTest` (three URL filters, `/q` VIEW only;
`noCameraIsAskedForOrDeclared`), `RedactionTest` (the live query). Mutations (2026-11-19): the `/q`
filter's path set to `/t` → `MergedManifestTest` red; a live QR sent as `sun` → `TapFlowTest` red.
