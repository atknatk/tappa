package scripttest

import (
	"strings"
	"testing"
)

// M16-02 (ADR 0045 §3): redline-check.sh sees the native staff apps under mobile/ -- a directory
// that does not exist yet -- and the ADR 0017 relay under android/. Every pattern the card added
// fires on a violation (one file each, so each is named on its own) and stays quiet on its control,
// the API the app is meant to use instead.
//
// THE SPELLINGS ARE MASKED IN THIS FILE: it lives under cmd/, which the real script scans, so a
// violation written out here would turn the repository red (redline_api3_test.go's rule). Every
// fixture carries "~~" inside its trigger and its log call, and m16Unmask removes it at run time.
// The comments below therefore describe the card's names instead of spelling them.
var m16Unmask = strings.NewReplacer("~~", "")

func m16Tree(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for rel, src := range files {
		out[rel] = m16Unmask.Replace(src)
	}
	return out
}

// m16RedGreen runs the red tree and the green tree once each: the red tree must exit non-zero with
// every one of its files named under rule's FAIL, the green tree must exit 0 with no FAIL at all.
func m16RedGreen(t *testing.T, rule string, red, green map[string]string) {
	t.Helper()
	out, code := redlineRun(t, m16Tree(red))
	if code == 0 {
		t.Errorf("%s: the violations' tree exited 0:\n%s", rule, out)
	}
	section := failSection(out, rule)
	for rel := range red {
		if !strings.Contains(section, rel+":") {
			t.Errorf("%s: %s is not named under %s FAIL:\n%s", rule, rel, rule, out)
		}
	}
	out, code = redlineRun(t, m16Tree(green))
	if code != 0 || strings.Contains(out, "· FAIL]") {
		t.Errorf("%s: the controls' tree: want exit 0 and no FAIL, got exit %d:\n%s", rule, code, out)
	}
}

const (
	m16iOS     = "mobile/ios/Taptime/"
	m16Android = "mobile/android/app/src/main/"
)

// TestRedline_M16_MobileIsScannedOnceItExists pins SRC's one optional entry. An absent mobile/ is
// not a scan error -- every other SRC entry is (a missing path makes rg exit 2 and the script
// exit 2, measured) -- but it is reported, so "the apps were scanned and are clean" and "there
// were no apps" do not print the same thing. Once a file exists under mobile/ it is scanned with no
// switch to turn on.
func TestRedline_M16_MobileIsScannedOnceItExists(t *testing.T) {
	t.Parallel()
	const absent = "mobile/ yok"

	out, code := redlineRun(t, nil)
	if code != 0 || !strings.Contains(out, absent) || strings.Contains(out, "ATLANDI") {
		t.Errorf("no mobile/: want exit 0, the absence reported and no scan error, got exit %d:\n%s", code, out)
	}

	out, code = redlineRun(t, m16Tree(map[string]string{m16iOS + "Gate.swift": "import Local~~Authentication\n"}))
	if code == 0 || !strings.Contains(failSection(out, "R1"), m16iOS+"Gate.swift:1:") {
		t.Errorf("a mobile/ file with a violation: want an R1 FAIL naming it, got exit %d:\n%s", code, out)
	}
	if strings.Contains(out, absent) {
		t.Errorf("mobile/ exists but the script still reports it absent:\n%s", out)
	}

	out, code = redlineRun(t, map[string]string{m16iOS + "App.swift": "import SwiftUI\n"})
	if code != 0 || strings.Contains(out, absent) || strings.Contains(out, "ATLANDI") {
		t.Errorf("a clean mobile/: want exit 0, no absence line and no scan error, got exit %d:\n%s", code, out)
	}
}

// TestRedline_M16_MobileR1PlatformAPIs pins R1's platform patterns: every one of the card's
// fourteen names, including the five the old roots already caught (measured, not repeated in the
// trigger), in the file kind it would really appear in. The controls are what the app's device key
// is meant to use: a Keychain accessibility class, the Secure Enclave's private-key-usage flag,
// StrongBox and the unlocked-device requirement on Android -- none of them asks for a person.
func TestRedline_M16_MobileR1PlatformAPIs(t *testing.T) {
	t.Parallel()
	red := map[string]string{
		m16iOS + "r1_framework.swift":       "import Local~~Authentication\n",
		m16iOS + "r1_context.swift":         "let c = LACon~~text()\n",
		m16iOS + "r1_any.swift":             "let f: SecAccessControlCreateFlags = .bio~~metryAny\n",
		m16iOS + "r1_current.swift":         "let f: SecAccessControlCreateFlags = .bio~~metryCurrentSet\n",
		m16iOS + "r1_presence.swift":        "let f: SecAccessControlCreateFlags = .user~~Presence\n",
		m16iOS + "r1_usage.plist":           "<dict>\n<key>NSFace~~IDUsageDescription</key>\n<string>x</string>\n</dict>\n",
		m16iOS + "r1_passkey.swift":         "let p = ASAuthorizationPlatformPublicKey~~CredentialProvider(relyingPartyIdentifier: \"taptime.eu\")\n",
		m16Android + "kotlin/r1_prompt.kt":  "val p = Bio~~metricPrompt(this, executor, callback)\n",
		m16Android + "kotlin/r1_package.kt": "import androidx.bio~~metric.Bio~~metricManager\n",
		m16Android + "kotlin/r1_keystore.kt": "val spec = KeyGenParameterSpec.Builder(alias, PURPOSE_SIGN)\n" +
			"    .setUserAuthentication~~Required(true)\n",
		m16Android + "kotlin/r1_manager.kt": "import androidx.creden~~tials.CredentialManager\n",
		m16Android + "kotlin/r1_create.kt":  "val r = CreatePublicKey~~CredentialRequest(json)\n",
		m16Android + "r1_usebio.xml":        "<uses-permission android:name=\"android.permission.USE_BIO~~METRIC\"/>\n",
		m16Android + "r1_finger.xml":        "<uses-permission android:name=\"android.permission.USE_FINGER~~PRINT\"/>\n",
	}
	green := map[string]string{
		m16iOS + "DeviceKey.swift": "let access = SecAccessControlCreateWithFlags(nil,\n" +
			"    kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .privateKeyUsage, nil)\n" +
			"// the phone's own screen lock is not our layer\n",
		m16Android + "kotlin/DeviceKey.kt": "val spec = KeyGenParameterSpec.Builder(alias, PURPOSE_SIGN)\n" +
			"    .setIsStrongBoxBacked(true)\n    .setUnlockedDeviceRequired(true)\n    .build()\n" +
			"val credentials = \"same-origin\"\n",
		// Near misses: an identifier that only ends in the context class's letters, a word that
		// only begins like the framework.
		m16iOS + "Near.swift": "let formulaContext = 1\nlet localAuthority = \"x\"\n",
	}
	m16RedGreen(t, "R1", red, green)
}

// TestRedline_M16_MobileBackgroundLocation pins R2's platform patterns: each continuous,
// background or region-monitoring path the card names, and the background-modes key holding the
// location mode in all four ways it is written (a plist array among other modes, a build setting
// on one line, a project-file array split over lines, a YAML block list). The controls are the
// one-shot read the app uses, its When-In-Use permission, other background modes, a location
// string under another key, a non-location foreground service, an unrelated start-monitoring call
// without the region argument, and an identifier that only begins with the live-updates name.
func TestRedline_M16_MobileBackgroundLocation(t *testing.T) {
	t.Parallel()
	red := map[string]string{
		m16Android + "r2_bg.xml":         "<uses-permission android:name=\"android.permission.ACCESS_BACKGROUND_~~LOCATION\"/>\n",
		m16Android + "r2_fgsperm.xml":    "<uses-permission android:name=\"android.permission.FOREGROUND_SERVICE_~~LOCATION\"/>\n",
		m16Android + "r2_fgstype.xml":    "<service android:name=\".S\" android:foregroundServiceType=\"camera|loc~~ation\"/>\n",
		m16Android + "kotlin/r2_fgs.kt":  "startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_~~LOCATION)\n",
		m16Android + "kotlin/r2_upd.kt":  "lm.requestLocation~~Updates(LocationManager.GPS_PROVIDER, 0L, 0f, listener)\n",
		m16Android + "kotlin/r2_geo.kt":  "val c = LocationServices.getGeo~~fencingClient(this)\n",
		m16iOS + "r2_always.swift":       "manager.requestAlways~~Authorization()\n",
		m16iOS + "r2_bgupd.swift":        "manager.allowsBackgroundLocation~~Updates = true\n",
		m16iOS + "r2_slc.swift":          "manager.startMonitoringSignificantLocation~~Changes()\n",
		m16iOS + "r2_visits.swift":       "manager.startMonitoring~~Visits()\n",
		m16iOS + "r2_region.swift":       "manager.startMonitoring~~(for: region)\n",
		m16iOS + "r2_circular.swift":     "let r = CLCircular~~Region(center: c, radius: 150, identifier: \"door\")\n",
		m16iOS + "r2_updating.swift":     "manager.startUpdating~~Location()\n",
		m16iOS + "r2_monitor.swift":      "let m = await CLMoni~~tor(\"taptime\")\n",
		m16iOS + "r2_update.swift":       "let stream = CLLocation~~Update.self\n",
		m16iOS + "r2_live.swift":         "for try await u in .live~~Updates(.otherNavigation) { _ = u }\n",
		m16iOS + "r2_session.swift":      "let s = CLBackgroundActivity~~Session()\n",
		m16iOS + "r2_both.plist":         "<key>NSLocationAlways~~AndWhenInUseUsageDescription</key>\n",
		m16iOS + "r2_old.plist":          "<key>NSLocationAlways~~UsageDescription</key>\n",
		m16iOS + "r2_modes.plist":        "<dict>\n<key>UIBackground~~Modes</key>\n<array>\n\t<string>fetch</string>\n\t<string>location</string>\n</array>\n</dict>\n",
		"mobile/ios/A.xcodeproj/r2_line": "INFOPLIST_KEY_UIBackground~~Modes = location;\n",
		"mobile/ios/A.xcodeproj/r2_arr":  "UIBackground~~Modes = (\n\t\tfetch,\n\t\tlocation,\n\t);\n",
		"mobile/ios/r2_project.yml":      "info:\n  properties:\n    UIBackground~~Modes:\n      - fetch\n      - location\n",
	}
	green := map[string]string{
		m16iOS + "Locate.swift": "manager.requestWhenInUseAuthorization()\nmanager.requestLocation()\n" +
			"reachability.startMonitoring()\nvar liveUpdatesEnabled = false\n",
		m16iOS + "Info.plist": "<dict>\n<key>NSLocationWhenInUseUsageDescription</key>\n<string>Read once, at the tap.</string>\n" +
			"<key>UIBackgroundModes</key>\n<array>\n\t<string>remote-notification</string>\n</array>\n" +
			"<key>TTExampleModes</key>\n<array>\n\t<string>location</string>\n</array>\n</dict>\n",
		m16Android + "AndroidManifest.xml": "<uses-permission android:name=\"android.permission.ACCESS_FINE_LOCATION\"/>\n" +
			"<uses-permission android:name=\"android.permission.ACCESS_COARSE_LOCATION\"/>\n" +
			"<service android:name=\".S\" android:foregroundServiceType=\"dataSync\"/>\n",
		m16Android + "kotlin/Locate.kt": "client.getCurrentLocation(Priority.PRIORITY_HIGH_ACCURACY, cancel.token)\n" +
			"lm.removeLocationUpdates(listener)\n",
		// Near misses: the stop calls, a plain foreground-service permission, a map region.
		m16iOS + "Near.swift": "manager.stopUpdatingLocation()\nmanager.stopMonitoringVisits()\n" +
			"let r = MKCoordinateRegion(center: c, latitudinalMeters: 300, longitudinalMeters: 300)\n",
		m16Android + "Near.xml": "<uses-permission android:name=\"android.permission.FOREGROUND_SERVICE\"/>\n",
	}
	m16RedGreen(t, "R2", red, green)
}

// TestRedline_M16_MobileLogCalls pins R7's mobile log arm: Swift's os_log, a Logger instance,
// print, debugPrint and NSLog, and Kotlin's android.util.Log, println and System.out -- each with
// one class of CLAUDE.md §7's list as M16-01 widened it (token, live QR token, device signature,
// request nonce, pairing and approval codes, session cookie, coordinate, latitude, e-mail, invite
// code), on one line and across lines, inside a Swift interpolation (with a private-privacy
// marker too). The controls are log lines that carry none of them -- the relay's own shapes among
// them -- and a value bound outside any log call.
func TestRedline_M16_MobileLogCalls(t *testing.T) {
	t.Parallel()
	red := map[string]string{
		m16iOS + "r7_token.swift":            "log~~ger.info(\"session \\(to~~ken)\")\n",
		m16iOS + "r7_private.swift":          "log~~ger.info(\"t=\\(to~~ken, privacy: .private)\")\n",
		m16iOS + "r7_signature.swift":        "os_~~log(\"%{public}@\", log: .default, type: .info, sig~~nature)\n",
		m16iOS + "r7_nonce.swift":            "pri~~nt(\"sent \\(count) with \\(non~~ce)\")\n",
		m16iOS + "r7_approval.swift":         "Self.log~~ger.debug(\"approval \\(approval~~Code)\")\n",
		m16iOS + "r7_coordinate.swift":       "log~~ger.notice(\"at \\(location.coord~~inate)\")\n",
		m16iOS + "r7_email.swift":            "NS~~Log(\"user %@\", user.e~~mail)\n",
		m16iOS + "r7_cookie.swift":           "debug~~Print(\"jar\", session.coo~~kie)\n",
		m16iOS + "r7_multi.swift":            "log~~ger.error(\n    \"failed \\(error) for \\(non~~ce)\"\n)\n",
		m16Android + "kotlin/r7_qr.kt":       "Lo~~g.d(TAG, \"qr=$liveQr~~Token\")\n",
		m16Android + "kotlin/r7_sig.kt":      "print~~ln(\"sig=$si~~g\")\n",
		m16Android + "kotlin/r7_pairing.kt":  "Lo~~g.i(TAG, \"pairing_~~code=$c\")\n",
		m16Android + "kotlin/r7_lat.kt":      "Lo~~g.w(TAG, \"at ${loc.lati~~tude}\")\n",
		m16Android + "kotlin/r7_invite.kt":   "System.out.print~~ln(\"invite_~~code=\" + c)\n",
		m16Android + "kotlin/r7_email.kt":    "Lo~~g.e(\n    TAG,\n    \"send failed for ${user.e~~mail}\",\n    e,\n)\n",
		m16Android + "kotlin/r7_activate.kt": "Lo~~g.v(TAG, \"activation_~~code=$a\")\n",
	}
	green := map[string]string{
		m16iOS + "Tap.swift": "logger.info(\"tap sent, verdict \\(verdict)\")\nprint(\"exchanges \\(count)\")\n" +
			"os_log(\"tap screen shown\")\nlet token = try await api.activate()\n",
		m16Android + "kotlin/Tap.kt": "Log.i(TAG, \"sign-out answered HTTP $status\")\n" +
			"Log.i(TAG, \"server check: HTTP ${p.status} location=${p.location}\")\n" +
			"Log.d(TAG, \"step ${step.name}\")\nval tokenUrl = base + \"/app/v1/activate\"\n",
		// Near misses for the word-bounded and whole-word triggers.
		m16iOS + "Near.swift": "print(\"design \\(n)\")\nprint(\"flat \\(n)\")\nlogger.info(\"coordinator started\")\n",
	}
	m16RedGreen(t, "R7", red, green)
}

// TestRedline_M16_AndroidRelayIsScanned pins the card's android/ decision: the ADR 0017 relay
// joined SRC (measured: zero hits on the shipped relay for every rule), so R1's prompt class, a
// background-location permission and a logged CMAC there now fail -- before M16-02 all three exited
// 0. The controls are the relay's own log lines and permissions.
func TestRedline_M16_AndroidRelayIsScanned(t *testing.T) {
	t.Parallel()
	const relay = "android/app/src/main/kotlin/mt/taptime/relay/"
	red := map[string]string{
		relay + "r1.kt":                          "val p = Bio~~metricPrompt(this, executor, callback)\n",
		"android/app/src/main/r2_manifest.xml":   "<uses-permission android:name=\"android.permission.ACCESS_BACKGROUND_~~LOCATION\"/>\n",
		relay + "r7.kt":                          "Lo~~g.d(TAG, \"round: cm~~ac=$c\")\n",
		"android/app/src/main/r2_foreground.xml": "<service android:foregroundServiceType=\"loc~~ation\"/>\n",
	}
	green := map[string]string{
		relay + "Main.kt": "Log.i(TAG, \"round: exchange $n after step $step\")\n" +
			"Log.i(TAG, \"round: ended as ${outcome.javaClass.simpleName} after ${outcome.exchanges} exchanges\")\n",
		"android/app/src/main/AndroidManifest.xml": "<uses-permission android:name=\"android.permission.NFC\"/>\n" +
			"<uses-permission android:name=\"android.permission.INTERNET\"/>\n",
	}
	out, code := redlineRun(t, m16Tree(red))
	if code == 0 {
		t.Errorf("the relay's violations exited 0:\n%s", out)
	}
	for rule, rel := range map[string]string{
		"R1": relay + "r1.kt", "R7": relay + "r7.kt",
		"R2": "android/app/src/main/r2_manifest.xml",
	} {
		if !strings.Contains(failSection(out, rule), rel+":") {
			t.Errorf("%s is not named under %s FAIL:\n%s", rel, rule, out)
		}
	}
	if !strings.Contains(failSection(out, "R2"), "android/app/src/main/r2_foreground.xml:") {
		t.Errorf("the relay's location foreground service is not named under R2 FAIL:\n%s", out)
	}
	out, code = redlineRun(t, green)
	if code != 0 || strings.Contains(out, "· FAIL]") {
		t.Errorf("the relay's own shapes: want exit 0 and no FAIL, got exit %d:\n%s", code, out)
	}
}

// TestRedline_M16_MobileB1 pins B1 over mobile/: the product name in every kind of file an app
// shows a person -- Android strings, manifest label and Kotlin literals; Swift literals (a
// multi-line one too), .strings tables, the String Catalog, Info.plist, a storyboard and the
// project file's product name -- and, the other way, that the XML-comment exemption is bound to
// mobile/ itself (a served web/static/mobile/ file is text, comments included). The green tree is
// what is not user text: comments in each kind, a Swift backtick identifier (not a string in
// Swift), documents and configuration, the three test-source layouts and a Gradle script.
func TestRedline_M16_MobileB1(t *testing.T) {
	t.Parallel()
	red := map[string]string{
		m16Android + "res/values/strings.xml":          "<resources>\n  <string name=\"app_name\">Tappa</string>\n</resources>\n",
		m16Android + "AndroidManifest.xml":             "<manifest>\n  <application android:label=\"Tappa\"/>\n</manifest>\n",
		m16Android + "kotlin/Wording.kt":               "package zz\n\nval title = \"Tappa check-in\"\n",
		m16iOS + "TapView.swift":                       "import SwiftUI\n\nlet t = Text(\"Welcome to Tappa\")\n",
		m16iOS + "Raw.swift":                           "let s = \"\"\"\nfirst\nTappa second\n\"\"\"\n",
		m16iOS + "en.lproj/Localizable.strings":        "/* greeting */\n\"welcome\" = \"Welcome to Tappa\";\n",
		m16iOS + "en.lproj/InfoPlist.strings":          "\"CFBundleDisplayName\" = \"Tappa\";\n",
		m16iOS + "Localizable.xcstrings":               "{\n  \"strings\": {\"welcome\": {\"localizations\": {\"en\": {\"stringUnit\": {\"value\": \"Tappa\"}}}}}\n}\n",
		m16iOS + "Info.plist":                          "<key>CFBundleDisplayName</key>\n<string>Tappa</string>\n",
		m16iOS + "Base.lproj/LaunchScreen.storyboard":  "<scene>\n  <label text=\"Tappa\"/>\n</scene>\n",
		"mobile/ios/Taptime.xcodeproj/project.pbxproj": "\t\t\t\tPRODUCT_NAME = Tappa;\n",
		"web/static/mobile/zz.xml":                     "<!-- Tappa -->\n<x/>\n",
	}
	want := []string{
		m16Android + "res/values/strings.xml:2:",
		m16Android + "AndroidManifest.xml:2:",
		m16Android + "kotlin/Wording.kt:3:",
		m16iOS + "TapView.swift:3:",
		m16iOS + "Raw.swift:1:",
		m16iOS + "en.lproj/Localizable.strings:2:",
		m16iOS + "en.lproj/InfoPlist.strings:1:",
		m16iOS + "Localizable.xcstrings:2:",
		m16iOS + "Info.plist:2:",
		m16iOS + "Base.lproj/LaunchScreen.storyboard:2:",
		"mobile/ios/Taptime.xcodeproj/project.pbxproj:1:",
		"web/static/mobile/zz.xml:1:",
	}
	green := map[string]string{
		m16iOS + "Ok.swift": "// Tappa draws this\n/* Tappa\n   again */\nlet `Tappa` = 1\nenum Tappa {}\n" +
			"let t = Text(\"Taptime\") // Tappa\n",
		m16iOS + "en.lproj/Ok.strings":                   "/* Tappa */\n// Tappa\n\"k\" = \"Taptime\";\n",
		m16iOS + "Ok.plist":                              "<!-- Tappa -->\n<string>Taptime</string>\n",
		m16Android + "res/values/ok.xml":                 "<resources>\n  <!-- Tappa -->\n  <string name=\"x\">Taptime</string>\n</resources>\n",
		m16Android + "kotlin/Ok.kt":                      "package zz\n\n// the \"Tappa\" app\nval zz = \"Taptime\"\n",
		"mobile/README.md":                               "# Tappa mobile\n",
		"mobile/ios/README.md":                           "Tappa\n",
		m16iOS + "Assets.xcassets/Contents.json":         "{\"info\": {\"author\": \"Tappa\"}}\n",
		"mobile/ios/project.yml":                         "name: Tappa\n",
		"mobile/android/app/src/test/kotlin/X.kt":        "val s = \"Tappa\"\n",
		"mobile/android/app/src/androidTest/kotlin/X.kt": "val s = \"Tappa\"\n",
		"mobile/ios/TaptimeTests/X.swift":                "let s = \"Tappa\"\n",
		"mobile/ios/TaptimeUITests/X.swift":              "let s = \"Tappa\"\n",
		"mobile/android/settings.gradle.kts":             "rootProject.name = \"Tappa\"\n",
	}

	redOut, redCode := redlineRun(t, red)
	redFail := failSection(redOut, "B1")
	if redCode == 0 {
		t.Errorf("a tree that writes Tappa on every app surface exited 0:\n%s", redOut)
	}
	for _, w := range want {
		if !strings.Contains(redFail, w) {
			t.Errorf("B1 does not report %s; FAIL section:\n%s", w, redFail)
		}
	}
	if n := strings.Count(redFail, "\n    "); n != len(want) {
		t.Errorf("B1 reported %d finding(s), want %d (one per leak):\n%s", n, len(want), redFail)
	}
	greenOut, greenCode := redlineRun(t, green)
	if greenCode != 0 || failSection(greenOut, "B1") != "" {
		t.Errorf("the exemptions' tree: want exit 0 and no B1 FAIL, got exit %d:\n%s", greenCode, greenOut)
	}
}
