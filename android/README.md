# Taptime plaque relay — Android (M8-05 FAZ B3, code half)

The phone half of the plaque encode tool decided in
[ADR 0017](../docs/adr/0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md): an **APDU
relay**. The server (`internal/encode`, `internal/handler/plaqueencode.go`) holds the
EV2 session and builds every C-APDU; this app pushes those bytes at an NTAG 424 DNA
over `IsoDep` and posts the chip's answer back. It computes nothing and decides
nothing about the bytes.

🔴 **What it does and does not hold — the words matter (security audit, F1).** The
phone **holds no key**: no plaque key, no KEK, no session key is ever derived or
handed to it; the grep in *Measurements* re-checks the vocabulary. The phone is
**not free of secrets**: for the length of one round its memory carries a dump that
ADR 0017 §3 calls *equivalent to the key* — the C-APDUs of `authenticate.1/.2` and
`changekey.sdmfileread` are sealed under session keys that derive from the PUBLIC
factory key, so whoever reads that dump can recover that plaque's `K_SDMFileRead`
(ADR 0017 §2.2, ADR 0005 risk 7). Those bytes live in the JSON body string, the
`Progress.command` array, the R-APDU array and its hex form (`HttpRelayServer`,
`RelayServer`, `RelayLoop`), and in the system NFC service on the far side of the
Binder call; none of them is zeroed — a Kotlin `String` cannot be, a `ByteArray`
waits for the collector. ADR 0017 §2.2 tried six designs against this and closed
none; this app adds no seventh. It follows the ADR's line instead: encode in a
controlled place, with our device, before the plaque reaches a wall.

User-facing name **Taptime**, package `mt.taptime.relay` (K6). One screen, five
states: signed out → ready → writing → done / fault.

This directory is a separate Gradle build at the repository root, **outside the Go
module**: `go build ./...` never sees it and `scripts/redline-check.sh`'s fixed `SRC`
list does not scan it. It is **not** part of `make check`.

## Toolchain — measured on this machine (2026-09-18)

| Part | Version | Note |
|---|---|---|
| JDK | **21.0.10** (Homebrew `openjdk@21`, x86_64) | `make android*` pins `JAVA_HOME`; without it `gradle` on this machine picks Homebrew's JDK **25**, which was not measured |
| Gradle | **9.7.1** (system, `/usr/local/bin/gradle`) | **no wrapper JAR in the tree** — this repository carries no binaries |
| Android Gradle Plugin | **9.4.1** | its release notes state minimum Gradle **9.6.0**; measured green on 9.7.1 (`assembleDebug`, `testDebugUnitTest`) |
| Kotlin Gradle Plugin | **2.2.10** | AGP 9's **built-in** Kotlin — there is no `org.jetbrains.kotlin.android` plugin in these build files (applying one is an error on AGP 9) |
| `kotlin-stdlib` (runtime) | 2.2.10 | the only runtime dependency this build declares; `debugRuntimeClasspath` resolves it **plus its transitive `org.jetbrains:annotations:13.0`** — two artefacts on the phone, not one |
| compileSdk / targetSdk | **36** | `platforms;android-36` |
| minSdk | **26** | adaptive icon is text-only XML, so no PNGs are needed |
| Build Tools | 36.0.0 | AGP 9.4's minimum |
| platform-tools / adb | 37.0.1 | |
| Test-only dependencies | `junit:junit:4.13.2`, `org.json:json:20260814` | the real `org.json` so the reply parser — which runs on the platform's `org.json` on the phone — runs on the JVM |

`androidx.appcompat` / `androidx.webkit` were permitted (K7) and turned out
unnecessary: `android.app.Activity`, `android.webkit.WebView` + `CookieManager`,
`android.nfc.tech.IsoDep`, `java.net.HttpURLConnection` and `org.json` are all in
the platform. OkHttp / Retrofit / Moshi / Compose: none.

Memory is tight on the build machine: `gradle.properties` caps the daemon at
`-Xmx2g`, disables parallel builds and limits workers to 2. Do not run the Go test
suite while this builds.

## Build, install, test

```bash
make android        # assembleDebug + adb install -r   (JAVA_HOME / ANDROID_HOME pinned in the Makefile)
make android-test   # JVM unit tests — no hardware, no device

# or by hand
cd android
JAVA_HOME=/usr/local/opt/openjdk@21 ANDROID_HOME=/usr/local/share/android-commandlinetools \
  gradle assembleDebug testDebugUnitTest
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n mt.taptime.relay/.MainActivity
adb logcat -s TaptimeRelay
```

`gradle test` also works (it additionally compiles the release variant). Build
output, `.gradle/`, `local.properties` and `*.apk` are ignored by the root
`.gitignore`; only source is committed.

Sizes are stated for a **clean** build (`rm -rf app/build .gradle .kotlin` first);
an incremental build packages differently and an earlier report quoted one
(944 796 B against the auditor's 913 701 B — both were real, neither was clean).
Clean `assembleDebug`, 2026-09-18, round-3 source: **915 285 B** (round 2 measured 914 557 and 914 561 B from one source — the APK carries a timestamp; the figure is a size class, not an identity).

## The wire contract (the server's, not ours)

Three routes, all `POST`, form-encoded in, JSON out
(`internal/handler/plaqueencode.go`):

```
POST /admin/plaques/encode          (empty body — tenant and actor come from the panel session)
POST /admin/plaques/encode/step     session=<handle>&rapdu=<hex>
POST /admin/plaques/encode/abort    session=<handle>
```

Success: `{"session","command","step","done"}` — `command` is the next C-APDU as
hex and is empty exactly when the round is over. Fault: `{"fault": <word>}` with a
matching status (`encode-unavailable` 503 · `bad-request` 400 · `unknown-session`
404 · `busy` 409 · `refused` 422 · `too-many-rounds` 429 · `server-error` 500).
Anything non-JSON is the panel chain ahead of the endpoint (303 sign-in / origin,
429 HTML) and **only its status code is read**.

Ten exchanges today (`select · getversion.1-3 · authenticate.1-2 · getcarduid ·
writedata · changekey.sdmfileread · changefilesettings`), eleven when ADR 0017 §5.1
step 8 ships. The app takes its cue from `done`, never from a count.

`ContractPinTest` reads the **Go source** two directories up and pins the three
routes, the seven fault words, the ten step names, the two form field names, the
panel cookie name and path, the login path, the deployed `TAPPA_BASE_URL` and the
two wording milestones (row written after exchange 4, secret installed after 9) against
the Kotlin constants. A drift on the server turns the Android tests red **under one
condition: the five server files are declared as inputs of the unit-test task**
(`app/build.gradle.kts`, `test.inputs.files(...)`). Without that declaration the
sentence was false on a warm tree and an audit measured it — see *Measurements*.
`everyFileThisTestReadsIsATaskInput` requires every path the test opens to appear
in the build file, so the two lists cannot drift apart silently. The reading is
text with comment lines stripped, not AST; a code line carrying a
`faultX = "…"`-shaped *trailing* comment would still match (false red, never false
green).

### Two headers are the contract

- **`Origin: <server address>`** — the routes mount under `AdminAuth.ProtectWriting`,
  whose `sameOriginGate` refuses a request carrying neither an `Origin` equal to
  `TAPPA_BASE_URL` nor a `Sec-Fetch-Site`. A native client sends neither by default
  (m8-deploy-pilot.md open item 20). `HttpRelayServer` sets it on every request;
  it is derived from the configured address (scheme + authority only — an address
  with a path is refused at "Save address").
- **`Cookie`** — the panel session as the WebView sign-in left it in `CookieManager`.
  No API login, no token, no password held by the app (K3, ADR 0017 §6 md. 10) —
  and the WebView is opted out of the platform's Autofill
  (`IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS`) so that Samsung Pass / Google
  Password Manager cannot offer to keep the admin's password in a device or cloud
  store outside the app (audit F2; the sign-in form's `autocomplete` attributes are
  right for a desktop and were not touched). Not measured on a live sign-in (out of
  bounds against production); what the device does show: `dumpsys autofill`'s
  history has, for the round-1 local sign-in, a Samsung Pass session with a fill
  request on a field (`f=…:i65537`), and for the round-3 opening of the sign-in
  page after the opt-out, `f=0`, `No sessions`. Suggestive, not a proof — the field
  semantics of that dump are undocumented.

Redirects are **not** followed: a 303 is the signal.

### The `done` rule — and what the wire carries today

`encode.Progress.Done`: *"READ Done BEFORE READING THE ERROR."* A round can be done
on silicon and still fail after it (marking the row). `Reply.parse` therefore reads
`done` **first** — before `fault`, before the status code — and the loop reports
such a round as **completed**, never re-runs it and never aborts it. Mutations
M2/M13 below are the proof.

🔴 **Server-side gap, named:** today's server never puts a fault word in a done
body. `plaqueencode.go`'s `p.Done` arm answers with the plain four-field reply and
keeps the failure in its log, so `Reply.Progress.trailingFault` is **never filled**
by the current server and the "encoded — but the server reported …" sentence in
`Wording` cannot appear yet. The client is ready for the shape the contract
describes; the wire does not carry it. Recorded by the orchestrator as server work;
not this app's to fix (Go code is out of scope).

### No abort on a server fault — the reason per word

`Store.Abort` retires a live handle and *expires* a busy one, so "the server already
retired it" is true for only two of the seven words. The loop sends no abort on any
fault, and `RelayLoop`'s class comment gives the reason per word: `refused` /
`unknown-session` — already retired or never existed; `too-many-rounds` /
`server-error` / `encode-unavailable` — the refusal came from a gate ahead of the
handler and an abort is refused by the same gate, the session dies at the server's
90 s TTL (encodeGate's own "LET IT DIE"); `busy` — a live handle with a step in
flight is somebody else's round, and an abort would expire it; `bad-request` — this
client's wire shape is wrong and a further request on it is a guess (cost: a
per-plaque lock for up to 90 s).

## Measurements

### Origin — JVM vs phone, and this was the first thing caught

The JVM test `originAndCookie_onAllThreeRoutes` went **red on first run**: the
desktop JDK's `HttpURLConnection` keeps an applet-era list of *restricted* request
headers — `Origin` among them — and silently drops them at the socket. The unit-test
JVM now sets `sun.net.http.allowRestrictedHeaders=true` (in `app/build.gradle.kts`,
with the reason) so the test can see what the client sets.

Android's `HttpURLConnection` is OkHttp-backed and has no such list — **measured on
the Galaxy A15, not assumed**. The signed-out **Check server** button sends one POST
to the encode route with `Origin` and **no cookie** (it cannot open a round: no
session → `requireAdmin` answers first). Against the live server:

```
curl POST /admin/plaques/encode, no Origin, no cookie        -> 303  Location: /admin        (sameOriginGate)
curl POST ..., Origin: https://evil.example, no cookie        -> 303  Location: /admin        (sameOriginGate)
curl POST ..., Origin: https://taptime.mt, no cookie          -> 303  Location: /admin/login  (requireAdmin)
phone, Check server -> logcat: "server check: HTTP 303 location=/admin/login originAccepted=true"
```

The phone landed on `/admin/login`, i.e. the origin gate let it through — the header
was on the wire.

### Sign-in through the WebView, on the device, against a local server

Local Go server on `:8080` with `TAPPA_BASE_URL=http://localhost:8080`, reached from
the phone through `adb reverse tcp:8080 tcp:8080` (so the phone's address and the
server's base URL are the same string, which is what the Origin check needs). Seed
owner credentials (`test/fixtures/seed.sql`). Measured:

```
GET  /admin/login 200 -> WebView renders the panel's sign-in (no script needed)
POST /admin/login 303 -> GET /admin 200        (server log)
CookieManager returned the HttpOnly panel cookie -> screen READY, reader mode on
    (dumpsys nfc: reader_mode_change flags: 385 = NFC_A | SKIP_NDEF_CHECK | NO_PLATFORM_SOUNDS)
relaunch: GET /admin 200 (startup probe) -> READY without a second sign-in   (the 12 h disk cookie — counted under "debug APK")
Sign out: POST /admin/logout 303; admin_sessions.revoked_at set; reader mode off (flags: 0); cookie jar cleared
```

`CookieManager.getCookie()` returning an **HttpOnly** cookie is documented platform
behaviour and is what K3 relies on; it was measured here rather than assumed.

A defect found only on the device: `closeWebView()` loads `about:blank`, whose
`onPageFinished` re-entered the signed-in branch and looped, re-enabling reader mode
every ~40 ms. Guarded (only while the sign-in page is showing, only for a page on the
panel's origin). It is written down in `MainActivity` at the spot.

### Contract pin — blind on a warm tree until the inputs were declared (audit B1)

Reproduced in a path-preserving replica of the checkout (`rsync` minus `.git`),
round-1 build file, `testDebugUnitTest` once green, then six drifts in the Go source
with the suite re-run each time:

```
BEFORE (no inputs declared)                          AFTER (inputs declared, same warm tree)
a  plaqueEncodeStepHref -> …/step2   UP-TO-DATE 31/0   FAILED  theThreeRoutes_matchPlaqueencodeGo
b  faultBusy -> "busy2"              UP-TO-DATE 31/0   FAILED  theFaultVocabulary_matchesPlaqueencodeGo
c  "getcarduid" -> "getcarduid2"     UP-TO-DATE 31/0   FAILED  theTenStepNames_matchDriverGo
d  an eleventh roundSteps entry      UP-TO-DATE 31/0   FAILED  theTenStepNames_matchDriverGo
e  PostFormValue("rapdu"->"response") UP-TO-DATE 31/0  FAILED  theTwoFormFields_matchWhatTheHandlersRead
f  CookieName -> "…_session2"        UP-TO-DATE 31/0   FAILED  thePanelCookieAndPaths_matchTheServer
g  a Go COMMENT `faultExample = "…"` (N5)              re-ran, 31/0 green (comment lines stripped)
h  cookie.go dropped from the inputs list              FAILED  everyFileThisTestReadsIsATaskInput
   nothing changed, second run                         UP-TO-DATE (declared inputs, not upToDateWhen{false})
```

### What the phone never holds — and what it does hold for one round

**Never:** a key as a key. No AES, no CMAC, no crypto, no key material, no session
key; nothing to zero because nothing was derived. That is what the grep below
measures — the **vocabulary** of the source, not the bytes in memory.

**For one round, and counted rather than denied:** the sealed C-APDUs and the chip's
R-APDUs (a key-equivalent dump under a public factory key, ADR 0017 §3), the round's
bearer handle, and the panel cookie in the HTTP client. In a debug build every one
of them is readable without root: the auditor's `adb shell am dumpheap
mt.taptime.relay …` produced a **39 078 721-byte heap dump** of the running app.
See *The debug APK is not an artefact for an operator*.

Run from `android/`:

```bash
# no secret-material vocabulary in the app. The ONE permitted substring, in main and
# test sources alike, is the server's own step name "changekey.sdmfileread" (pinned
# from driver.go; it names the wording milestone). Expect no output:
grep -rniE 'aes|cmac|crypto|key' app/src/main app/src/test app/build.gradle.kts build.gradle.kts settings.gradle.kts gradle.properties | grep -v 'changekey.sdmfileread'
# every Log.* call, and none of them may mention session, command, rapdu or cookie: expect no output from the second
grep -rnE 'Log\.[dviwe]\(' app/src/main
grep -rnE 'Log\.[dviwe]\(' app/src/main | grep -iE 'session|command|rapdu|cookie'
# the pure layer carries no logger at all: expect no output
grep -rlE 'android\.util\.Log|Log\.' app/src/main/kotlin | grep -v MainActivity
# the only persisted value is the server address: expect exactly one putString
grep -rnE 'putString|putExtra|putBoolean|putInt|edit\(\)' app/src/main
```

The session handle is a bearer credential. It lives in one local variable of
`RelayLoop.run()` for one round; no `Outcome` has a field for it, and
`Reply.Progress.toString()` redacts it and the command bytes.
`theHandleIsNeverInAnOutcomeAStringOrASentence` walks every `Outcome` field by
reflection to say so. Six `Log.i` lines exist, all in `MainActivity`: step names,
exchange counts, outcome class names, fault words, HTTP statuses.

### Mutation record (33 JVM tests; each mutation applied, suite run, source restored)

| # | Mutation | Red |
|---|---|---|
| M1 | drop the `Origin` header | `originAndCookie_onAllThreeRoutes` |
| M2 | parser reads `fault` before `done` | minimal form (fault block moved above the done block, empty-word check kept): **2** — `doneWithAFaultInTheSameBody_isCompletedAndNeverRerun`, `done_isReadBeforeFaultAndBeforeStatus`. The round-1 form also dropped the empty-word check and took `shapesThisClientDoesNotUnderstand_areMalformed` with it (3); the count is the mutation's, not the rule's |
| M3 | abort on a server fault | `everyFaultWord_stopsTheRound_withoutAnAbort_andHasItsOwnSentence` |
| M4 | no abort when the chip is lost | `chipLostMidRound_abortsExactlyOnce`, `originAndCookie_onAllThreeRoutes` |
| M5 | empty command treated as done | `emptyCommandWithoutDone_isAFaultAndAborts` |
| M6 | runaway guard removed | `serverThatNeverSaysDone_isStoppedAndAborted` (timeout) |
| M7 | `Progress.toString` prints the handle | `theHandleIsNeverInAnOutcomeAStringOrASentence` |
| M8 | cookie dropped | `originAndCookie_onAllThreeRoutes` |
| M9 | redirects followed | `refusal303_stopsWithoutAbort` |
| M10 | the probe sends the cookie | `probeOrigin_sendsOriginAndNoCookie_andReadsTheLocation` |
| M11 | a fault word misspelt | `theFaultVocabulary_matchesPlaqueencodeGo`, `everyFaultWord_…` |
| M12 | a route misspelt | `theThreeRoutes_matchPlaqueencodeGo` |
| M13 | loop turns done+fault into a fault | `doneWithAFaultInTheSameBody_isCompletedAndNeverRerun` |
| F4-1 | the `require(scheme == "https" \|\| allowHttp)` line dropped | `plainHttp_isRefusedUnlessTheBuildAllowsIt` |
| F4-2 | MainActivity passes `allowHttp = true` instead of `BuildConfig.DEBUG` | `theAppPassesBuildConfigDebugAtEveryCallSite` |
| F5 | host not lower-cased in `normaliseBase` | `origin_isTheBareLowerCaseOrigin` |
| F5b | scheme not lower-cased | `origin_isTheBareLowerCaseOrigin` |

## Testing against a local server

- The address on the screen **must equal the server's `TAPPA_BASE_URL`** — the
  Origin header is derived from it. For a LAN test run the server with
  `TAPPA_BASE_URL=http://<lan-ip>:8080`, or use `adb reverse tcp:8080 tcp:8080` and
  `http://localhost:8080` on both sides.
- Plain `http://` works in the **debug** build only, and the refusal is now the
  app's own (audit F4): `HttpRelayServer.normaliseBase(raw, allowHttp)` rejects it
  unless the caller allows, and the only caller in the app passes
  `BuildConfig.DEBUG` (`CleartextGateTest` reads `MainActivity.kt` and requires that
  at every call site). The platform's cleartext policy (`usesCleartextTraffic`, on
  only in `src/debug/AndroidManifest.xml`) remains as the second net. A stored
  address is re-validated under the running build's rule at start-up.
- The address is canonicalised to lower-case scheme and host (audit F5): Chromium
  reports every navigation with a lower-case host and the app decides "still on
  the panel?" with `startsWith(base)`, so `https://TapTime.mt` typed by hand used
  to block the post-sign-in 303 and sign-in never completed. The test now feeds
  raw-case input; the round-1 test lower-cased its own input and could not see it.
- A server whose base URL is not `https` starts **without** an encoder (measured:
  `"no plaque encode relay in this deployment … base URL must start with the https
  scheme"`), so every round there answers `encode-unavailable`. Sign-in and the
  Check probe still work; a real round needs an https base.

## NFC choices (K4)

- `enableReaderMode(FLAG_READER_NFC_A | FLAG_READER_SKIP_NDEF_CHECK |
  FLAG_READER_NO_PLATFORM_SOUNDS, null)`; default presence-check delay. Reader mode
  is enabled once per READY and disabled on pause / sign-out (`readerOn` guard;
  the pause cost is counted above).
- `IsoDep` stays connected for the whole round (Android has no session lifetime
  limit — the README's "decisive advantage over iOS"). The round runs entirely on
  the reader callback thread; `TagLostException` → one `abort` → the FAULT screen,
  whose advice depends on **how far the round got** (`Wording.afterInterruption`,
  three arms, each driven through the real loop in
  `chipLost_advice_dependsOnHowFarTheRoundGot`): below exchange 4 nothing exists
  and "try again" is right; from 4 the inventory row squats the uid and a retry
  comes back `refused`; from 9 the plaque's secret (application slot 0x01) is Tappa's with SDM still off
  — part-written, recoverable only through ADR 0017 §5.3's path, **which is not
  shipped**. The FAULT button reads "Next plaque" instead of "Try again" whenever
  the message says not to retry (`Wording.retryIsSafe`).
- **`onPause` switches reader mode off, and the cost is a round cut in two** by a
  call, the shade, a screen timeout: the next transceive throws TagLost and the
  arms above apply — at worst a part-written plaque. Whether there is an
  alternative is **not measured**: the auditor's AOSP reading is that
  `NfcActivityManager` ties reader mode to the activity's resumed state by itself,
  so not calling `disableReaderMode` would change nothing; measuring that needs a
  build without the call and a signed-in mid-round backgrounding, which was not
  done. The call stays explicit so the behaviour does not depend on the platform's.
- `setTimeout(5000)` per transceive. **Not a measurement**: `deploy/README.md` gives
  no number for the chip's slow steps, only that they ask for WTX, which the NFC
  controller honours below this layer. 5000 ms is the server's own per-exchange
  budget (`internal/encode`, `exchangeBudget = 5 s`) applied to the chip side. The
  hardware half measures the real figure.
- `getMaxTransceiveLength()` is checked before every transceive; a command longer
  than the phone's limit is a named fault, never a silent truncation.
- After DONE or FAULT the reader is **disarmed** until the operator taps *Next
  plaque* / *Try again*: a plaque left lying on the phone cannot start a second
  round by itself (a re-run of a completed plaque dies at the row insert and reads
  like stale inventory — `encode.Progress.Done`'s warning).

## Hardware half — what to expect first

Nothing here has touched silicon (m8-deploy-pilot.md open item 16). In order of
likelihood, the first things a real chip will show:

1. **Item 7 — step 5's CommMode.** The driver sends `WriteData` in CommMode.Full to a
   file whose `Write` right is free (`Eh`) at delivery; the datasheet says Plain in
   three places. Expect the round to fail at `writedata` (`refused`) with the NDEF
   file possibly written with sealed bytes. Fallback named in `driver.go`:
   `ISOUpdateBinary`, CommMode.Plain. The chip stays recoverable (no key changed yet).
2. **Item 8 — does the EV2 session survive `ChangeKey`?** The fake chip keeps it;
   `internal/sun/changekey.go` assumes the opposite for the key-0 case. Today's table
   changes key `0x01` and then runs `ChangeFileSettings` in the same session; if the
   chip drops the session after any `ChangeKey`, the round fails at
   `changefilesettings` — **after** the plaque's secret is installed, i.e. with SDM
   still off (fail-closed, ADR 0017 §5.1) and the row already written.
3. **Table 65's precondition** (`AUTHENTICATION_ERROR AEh`) on `ChangeKey` — the fake
   models it; silicon decides.
4. Timing: `TRANSCEIVE_TIMEOUT_MS` and the default presence check are unmeasured.
   If a `TagLostException` appears between exchanges with the plaque held still,
   set `NfcAdapter.EXTRA_READER_PRESENCE_CHECK_DELAY` in `enableReader()`'s extras.

## Brand — palette exact, typography not

The nine tokens are used verbatim (`res/values/colors.xml`); the stamp keeps the
brand's anatomy (word in ink, 2dp border + 1dp inner ring + 10 % ground in the
status colour, −6° tilt); the status → colour mapping is the fixed one. Two
deviations, named:

- **Typography.** Space Grotesk and IBM Plex Mono are **not** bundled; the layout
  uses the platform's `sans-serif` and `monospace` (Roboto and the system mono on
  the target phone). The roles are kept — sans for words, mono for data — the faces
  are not. Bundling would be the two families already vendored under
  `web/static/fonts/` as font resources (~77 KiB); not done in this round.
- **Hint contrast, fixed.** The address field's hint was `line` on `paper`
  (**1.52:1**, measured with the brand skill's method). It is now ink at 65 % over
  paper — an existing token's opacity, not a new one — which composites to
  `#676F66` and gives **5.10:1** (0.60 would give 4.34:1, under AA).

## The debug APK is not an artefact for an operator

The debug build is `debuggable`, so anybody with `adb` and the phone gets, without
root, **three things** (security audit, F1 — the first version of this section
counted only the first):

- **(a) the panel session** — `adb shell run-as mt.taptime.relay` lists the
  WebView's cookie store (`app_webview/Default/Cookies`, 24 576 B, plain SQLite
  behind the app sandbox and file-based encryption). It is also what a heap dump
  of the HTTP client carries.
- **(b) the live round's bearer handle** — `am dumpheap` during a round yields
  `session`; a second signed-in admin holding it can *drive somebody else's round*
  (ADR 0017 §6 md. 10, counted there; the round still writes into the opener's
  tenant).
- **(c) that plaque's `K_SDMFileRead`** — the same dump holds the
  `authenticate.1/.2` exchange and the `changekey.sdmfileread` command, which under
  the public factory key are the key (ADR 0017 §3, ADR 0005 risk 7).

So a debug APK on a phone anybody can plug in is a panel session, a handle and, mid
round, a plaque key anybody can lift. It exists for the hardware half and for this
machine's measurements, nothing else.

**The 12-hour cookie is part of this and is a product decision, written down rather
than locked (audit F3):** the panel cookie is `Max-Age` 12 h
(`internal/adminauth/cookie.go`, `cookieMaxAgeSeconds`), Chromium keeps it in that
SQLite file across app restarts, and *relaunch → READY without a second sign-in*
in *Measurements* is that behaviour seen from the front. ADR 0017 §2.2 already
counts it — *any phone carrying an admin's cookie can be a relay* — and this app
adds no device lock, PIN or shorter lifetime on top. Signing out revokes the
session on the server and clears the jar; a phone left signed in for the afternoon
is a phone that can encode for the afternoon.

🔴 **There is no release build yet.** A `make android-release` (signed,
`debuggable=false`, cleartext refused) is **not written**; the signing key is a
separate decision (where it lives, who holds it). Until then the operator artefact
does not exist — counted, not closed.

## Observed, not fixed

- `cmd/tappa`'s `TestEveryNamedTestExists` walks the whole checkout and skips only
  `.git · .tools · bin · node_modules`. Gradle output under `android/app/build/`
  (a zip-cache holding a kotlin-stdlib class whose name starts with the Go test prefix) made it red — measured,
  54 live against a budget of 53 — and it is green again with the output removed.
  Until that skip list learns `android/.gradle`, `android/build` and
  `android/app/build` (a Go test edit, outside this task), run `make test` with the
  Gradle output deleted, or expect that one test to name a cache file.

- Logcat shows `E t.taptime.relay: Invalid resource ID 0x00000000.` twice at
  startup (once at decor set-up, once as the WebView loads). The app runs and every
  screen renders; the line comes from framework code probing an optional theme
  attribute and names no resource of ours. Not chased.
- Google's developer verification (deploy/README.md, *"AMA BİR TARİH VAR"*): this app
  is installed with `adb install`, which is one of the two paths that stay open.
- `Hex.decode` accepted Unicode fullwidth digits in round 1 (`Character.digit`);
  it is ASCII-only now and the test carries a fullwidth case. Only ever mattered on
  the server→phone direction, where the server emits ASCII.
