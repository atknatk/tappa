# Taptime staff app — Android (M16-11, scaffold)

The optional native staff app decided in
[ADR 0045](../../docs/adr/0045-istege-bagli-native-uygulama.md), Android half. This card
is the **scaffold**: one empty screen ("Taptime / punchless"), the closed permission
list, no backup, and the build and test targets. Activation (M16-12) and the tap screen
(M16-13) come next; the browser path stays the default and is unchanged.

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

- **Mobile CI.** Q36 (d) decided that the Android JVM tests run in CI on a Linux runner.
  No workflow runs them yet; the job is assigned to **M16-12** (coordinator, 2026-11-18,
  review finding B-1).
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
