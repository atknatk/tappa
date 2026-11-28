#!/usr/bin/env bash
# Taptime iOS staff app (ADR 0045, M16-15) -- build, unit-test and measure the BUILT bundle.
#
# Three steps, in this order, and any failure stops the run:
#   1. `xcodebuild test` on a throwaway simulator this script creates and deletes;
#   2. an UNSIGNED Release build for a generic iOS device (CODE_SIGNING_ALLOWED=NO);
#   3. the compiled Info.plist of BOTH builds, and the entitlements, against allow-lists.
#
# WHY STEP 3 READS BUILD OUTPUT. redline R2 reads source text; an xcconfig, an
# INFOPLIST_KEY_ build setting or a generator can merge a key the source plist never shows
# (M16-02 audit). So the gate is the plist Xcode wrote: `plutil -extract UIBackgroundModes`
# must find nothing, and the only usage-description keys and the only NSLocation keys are
# the ones the card allows. Allow-lists, not deny-lists: a key nobody thought to forbid
# still fails, and this file never has to spell the forbidden names (redline R1/R2 scan
# scripts/ too).
#
# NO APPLE ACCOUNT IS TOUCHED. No -allowProvisioningUpdates: the simulator build signs
# "to run locally" (no certificate, no profile) and the device build is not signed at all.
# Installing on a phone is the device tour's job (M16-18).
#
# THE SIMULATOR IS OURS ONLY. Other projects' simulators run on this machine; the script
# creates one device with a unique name, and the trap shuts down and deletes exactly that
# UDID. Nothing here enumerates or stops other devices.
#
# Knobs (environment):
#   IOS_DERIVED_DATA     DerivedData directory (default mobile/ios/build, git-ignored)
#   IOS_SIM_RUNTIME      simulator runtime identifier (default: the newest available iOS)
#   IOS_SIM_DEVICE_TYPE  device type identifier (default: the runtime's first iPhone)
set -euo pipefail

cd "$(dirname "$0")/.."

PROJECT=mobile/ios/Taptime.xcodeproj
SCHEME=Taptime
SRC_ENTITLEMENTS=mobile/ios/Taptime/Taptime.entitlements
# The widget extension (M16-47): the home-screen widget and the lock-screen Live Activity.
WIDGET_SRC_ENTITLEMENTS=mobile/ios/TaptimeWidgets/TaptimeWidgets.entitlements
WIDGET_BUNDLE_ID=mt.taptime.app.widgets
APP_GROUP=group.mt.taptime.app
DD=${IOS_DERIVED_DATA:-mobile/ios/build}

BUNDLE_ID=mt.taptime.app
# iPhone 7's last iOS is 15.x (the CoreSimulator iPhone 7 profile carries maxRuntimeVersion
# 15.255.255; its last firmware is 15.8.8) and Xcode 26.3's iOS SDK accepts 15.0 as a
# deployment target (SDKSettings: MinimumDeploymentTarget 12.0). The pin is here so that a
# silent bump of IPHONEOS_DEPLOYMENT_TARGET fails the run instead of dropping iPhone 7.
MIN_OS=15.0
ALLOWED_USAGE=$'NFCReaderUsageDescription\nNSLocationWhenInUseUsageDescription'
ALLOWED_LOCATION=NSLocationWhenInUseUsageDescription
# The entitlements the card allows (associated domains, NFC reading -- value TAG, sources NDEF
# only -- and since M16-47 the App
# Group the widget reads) plus the three Xcode adds by itself when it processes them. App
# Attest is absent until Phase E (ADR 0045 2.7); aps-environment is absent for good (no push,
# M16-46 [G46-e]) -- an allow-list, so neither needs naming.
ALLOWED_SOURCE_ENTITLEMENTS=$'com.apple.developer.associated-domains\ncom.apple.developer.nfc.readersession.formats\ncom.apple.security.application-groups'
ALLOWED_BUILT_ENTITLEMENTS=$'application-identifier\ncom.apple.developer.associated-domains\ncom.apple.developer.nfc.readersession.formats\ncom.apple.developer.team-identifier\ncom.apple.security.application-groups\nget-task-allow'
# The extension: the App Group and nothing else.
ALLOWED_WIDGET_SOURCE_ENTITLEMENTS=com.apple.security.application-groups
ALLOWED_WIDGET_BUILT_ENTITLEMENTS=$'application-identifier\ncom.apple.developer.team-identifier\ncom.apple.security.application-groups\nget-task-allow'

fail() { echo "mobile-ios-test: FAIL: $*" >&2; exit 1; }

# The app reads the plaque's NDEF URL and nothing below it (CLAUDE.md 4.7): the TAG entitlement
# would allow a raw tag session (ISO 7816, MIFARE), so no Swift source may open one.
check_nfc_sources() {
  local hits
  hits=$(grep -rn -E 'NFCTagReaderSession|NFCISO7816Tag|NFCMiFareTag|NFCFeliCaTag|NFCISO15693Tag' \
    --include='*.swift' mobile/ios || true)
  [[ -z $hits ]] || fail "a Swift source goes below NDEF:"$'\n'"$hits"
  echo "  NFC sources: NDEF reader session only"
}
check_nfc_sources

# No push (M16-46 [G46-e]): the reminder is the phone's own local notification, so no source
# under mobile/ registers for remote notifications or adds a notification service extension.
# The entitlement half (no aps-environment) is the allow-lists below, on the built bundles too.
# Build output (mobile/ios/build, Gradle's build/ and .gradle/) is SDK and generated text, not ours.
check_push_sources() {
  local hits
  hits=$(grep -rn -E 'registerForRemoteNotifications|UNNotificationServiceExtension' \
    --exclude-dir=build --exclude-dir=.gradle mobile || true)
  [[ -z $hits ]] || fail "a source under mobile/ reaches for push:"$'\n'"$hits"
  echo "  push sources: none (no remote-notification registration, no service extension)"
}
check_push_sources

command -v xcodebuild >/dev/null || fail "xcodebuild not found (Xcode is required; the iOS build runs locally, Q36 d)"

# top_keys <plist>: the top-level keys, one per line, sorted. `plutil -p` indents the first
# level by exactly two spaces.
top_keys() {
  plutil -p "$1" | sed -n 's/^  "\([^"]*\)" =>.*/\1/p' | LC_ALL=C sort
}

check_info_plist() { # check_info_plist <label> <Info.plist>
  local label=$1 plist=$2 keys usage location
  [[ -f $plist ]] || fail "$label: no Info.plist at $plist"
  plutil -lint -s "$plist" || fail "$label: Info.plist does not parse"
  if plutil -extract UIBackgroundModes raw "$plist" >/dev/null 2>&1; then
    fail "$label: UIBackgroundModes is present in the compiled Info.plist (CLAUDE.md 4.2)"
  fi
  keys=$(top_keys "$plist")
  usage=$(grep 'UsageDescription$' <<<"$keys" || true)
  [[ $usage == "$ALLOWED_USAGE" ]] \
    || fail "$label: usage-description keys are not exactly the allowed two:"$'\n'"$usage"
  location=$(grep '^NSLocation' <<<"$keys" || true)
  [[ $location == "$ALLOWED_LOCATION" ]] \
    || fail "$label: NSLocation keys are not exactly When-In-Use:"$'\n'"$location"
  [[ $(plutil -extract CFBundleIdentifier raw "$plist") == "$BUNDLE_ID" ]] \
    || fail "$label: CFBundleIdentifier is not $BUNDLE_ID"
  [[ $(plutil -extract MinimumOSVersion raw "$plist") == "$MIN_OS" ]] \
    || fail "$label: MinimumOSVersion is not $MIN_OS (iPhone 7, see MIN_OS)"
  echo "  $label Info.plist: no UIBackgroundModes; usage keys = the allowed two; MinimumOSVersion $MIN_OS"
}

# Entitlement keys contain dots, which plutil's key paths cannot address; the values are
# checked on the JSON form instead (plutil writes it compact; key order does not matter to
# a substring test).
check_entitlements() { # check_entitlements <label> <plist> <allowed-keys>
  local label=$1 plist=$2 allowed=$3 keys extra json
  [[ -f $plist ]] || fail "$label: no entitlements at $plist"
  keys=$(top_keys "$plist")
  extra=$(LC_ALL=C comm -23 <(printf '%s\n' "$keys") <(printf '%s\n' "$allowed"))
  [[ -z $extra ]] || fail "$label: entitlements outside the allow-list:"$'\n'"$extra"
  json=$(plutil -convert json -o - "$plist")
  [[ $json == *'"com.apple.developer.associated-domains":["applinks:taptime.mt"]'* ]] \
    || fail "$label: associated domains are not exactly applinks:taptime.mt"
  # TAG, not NDEF: App Store Connect refuses NDEF for a 15.0 target (ITMS-90778, measured
  # 2026-11-27). TAG would also allow a raw tag session, so the sources are held to the NDEF
  # reader session below (check_nfc_sources).
  [[ $json == *'"com.apple.developer.nfc.readersession.formats":["TAG"]'* ]] \
    || fail "$label: NFC reader formats are not exactly TAG"
  [[ $json == *'"com.apple.security.application-groups":["'"$APP_GROUP"'"]'* ]] \
    || fail "$label: the App Group is not exactly $APP_GROUP"
  echo "  $label entitlements: $(tr '\n' ' ' <<<"$keys")"
}

check_widget_entitlements() { # check_widget_entitlements <label> <plist> <allowed-keys>
  local label=$1 plist=$2 allowed=$3 keys extra json
  [[ -f $plist ]] || fail "$label: no entitlements at $plist"
  keys=$(top_keys "$plist")
  extra=$(LC_ALL=C comm -23 <(printf '%s\n' "$keys") <(printf '%s\n' "$allowed"))
  [[ -z $extra ]] || fail "$label: entitlements outside the allow-list:"$'\n'"$extra"
  json=$(plutil -convert json -o - "$plist")
  [[ $json == *'"com.apple.security.application-groups":["'"$APP_GROUP"'"]'* ]] \
    || fail "$label: the App Group is not exactly $APP_GROUP"
  echo "  $label entitlements: $(tr '\n' ' ' <<<"$keys")"
}

# The extension's compiled Info.plist: the WidgetKit extension point and nothing else of note --
# no background mode, no usage description, the app's deployment target.
check_widget_plist() { # check_widget_plist <label> <Info.plist>
  local label=$1 plist=$2 keys
  [[ -f $plist ]] || fail "$label: no widget extension at $plist (not embedded?)"
  plutil -lint -s "$plist" || fail "$label: widget Info.plist does not parse"
  if plutil -extract UIBackgroundModes raw "$plist" >/dev/null 2>&1; then
    fail "$label: UIBackgroundModes is present in the widget's Info.plist (CLAUDE.md 4.2)"
  fi
  keys=$(top_keys "$plist")
  [[ -z $(grep -e 'UsageDescription$' -e '^NSLocation' <<<"$keys" || true) ]] \
    || fail "$label: the widget declares a usage description or a location key"
  [[ $(plutil -extract NSExtension.NSExtensionPointIdentifier raw "$plist") == com.apple.widgetkit-extension ]] \
    || fail "$label: the extension point is not com.apple.widgetkit-extension"
  [[ $(plutil -extract CFBundleIdentifier raw "$plist") == "$WIDGET_BUNDLE_ID" ]] \
    || fail "$label: the widget's CFBundleIdentifier is not $WIDGET_BUNDLE_ID"
  [[ $(plutil -extract MinimumOSVersion raw "$plist") == "$MIN_OS" ]] \
    || fail "$label: the widget's MinimumOSVersion is not $MIN_OS"
  # App Store validation refuses an extension whose versions are not its app's. Both come from
  # the PROJECT-level MARKETING_VERSION / CURRENT_PROJECT_VERSION; a target-level copy that
  # drifts fails here, on the built bundles.
  local app_plist key
  app_plist="$(dirname "$plist")/../../Info.plist"
  for key in CFBundleShortVersionString CFBundleVersion; do
    [[ -n $(plutil -extract "$key" raw "$app_plist") && \
       $(plutil -extract "$key" raw "$plist") == $(plutil -extract "$key" raw "$app_plist") ]] \
      || fail "$label: the widget's $key is not the app's"
  done
  echo "  $label widget Info.plist: widgetkit extension point; no UIBackgroundModes; no usage keys; the app's versions"
}

# --- 0. source entitlements (cheap, before any build) ---------------------------------
check_entitlements source "$SRC_ENTITLEMENTS" "$ALLOWED_SOURCE_ENTITLEMENTS"
check_widget_entitlements widget-source "$WIDGET_SRC_ENTITLEMENTS" "$ALLOWED_WIDGET_SOURCE_ENTITLEMENTS"

# --- 1. unit tests on a throwaway simulator ---------------------------------------------
runtimes=$(xcrun simctl list runtimes -j)
runtime=${IOS_SIM_RUNTIME:-}
if [[ -z $runtime ]]; then
  n=$(plutil -extract runtimes raw -o - - <<<"$runtimes")
  for ((i = 0; i < n; i++)); do
    [[ $(plutil -extract "runtimes.$i.platform" raw -o - - <<<"$runtimes") == iOS ]] || continue
    [[ $(plutil -extract "runtimes.$i.isAvailable" raw -o - - <<<"$runtimes") == true ]] || continue
    runtime=$(plutil -extract "runtimes.$i.identifier" raw -o - - <<<"$runtimes")
    ri=$i
  done
  [[ -n $runtime ]] || fail "no available iOS simulator runtime (xcrun simctl list runtimes)"
fi
devtype=${IOS_SIM_DEVICE_TYPE:-}
if [[ -z $devtype ]]; then
  if [[ -z ${ri:-} ]]; then
    n=$(plutil -extract runtimes raw -o - - <<<"$runtimes")
    for ((i = 0; i < n; i++)); do
      [[ $(plutil -extract "runtimes.$i.identifier" raw -o - - <<<"$runtimes") == "$runtime" ]] && ri=$i
    done
    [[ -n ${ri:-} ]] || fail "runtime $runtime is not installed"
  fi
  m=$(plutil -extract "runtimes.$ri.supportedDeviceTypes" raw -o - - <<<"$runtimes")
  for ((j = 0; j < m; j++)); do
    if [[ $(plutil -extract "runtimes.$ri.supportedDeviceTypes.$j.productFamily" raw -o - - <<<"$runtimes") == iPhone ]]; then
      devtype=$(plutil -extract "runtimes.$ri.supportedDeviceTypes.$j.identifier" raw -o - - <<<"$runtimes")
      break
    fi
  done
  [[ -n $devtype ]] || fail "runtime $runtime offers no iPhone device type"
fi

udid=$(xcrun simctl create "taptime-ios-test-$$" "$devtype" "$runtime")
cleanup() {
  xcrun simctl shutdown "$udid" >/dev/null 2>&1 || true # not booted is fine
  xcrun simctl delete "$udid" >/dev/null 2>&1 || echo "mobile-ios-test: could not delete simulator $udid" >&2
}
trap cleanup EXIT
echo "mobile-ios-test: simulator $udid ($devtype, $runtime)"

xcodebuild test -quiet -project "$PROJECT" -scheme "$SCHEME" -configuration Debug \
  -destination "id=$udid" -derivedDataPath "$DD" \
  || fail "xcodebuild test"
echo "  unit tests: passed"

# --- 2. unsigned device build ------------------------------------------------------------
xcodebuild build -quiet -project "$PROJECT" -scheme "$SCHEME" -configuration Release \
  -destination 'generic/platform=iOS' -derivedDataPath "$DD" CODE_SIGNING_ALLOWED=NO \
  || fail "xcodebuild build (generic iOS device, unsigned)"
echo "  device build (Release, unsigned): built"

# --- 3. the built bundles ----------------------------------------------------------------
check_info_plist simulator "$DD/Build/Products/Debug-iphonesimulator/Taptime.app/Info.plist"
check_info_plist device "$DD/Build/Products/Release-iphoneos/Taptime.app/Info.plist"
check_widget_plist simulator "$DD/Build/Products/Debug-iphonesimulator/Taptime.app/PlugIns/TaptimeWidgets.appex/Info.plist"
check_widget_plist device "$DD/Build/Products/Release-iphoneos/Taptime.app/PlugIns/TaptimeWidgets.appex/Info.plist"
# The simulator build signs locally and writes the entitlements Xcode processed; the
# unsigned device build writes none, so this is the one built list there is.
check_entitlements simulator-built \
  "$DD/Build/Intermediates.noindex/Taptime.build/Debug-iphonesimulator/Taptime.build/Taptime.app-Simulated.xcent" \
  "$ALLOWED_BUILT_ENTITLEMENTS"
check_widget_entitlements widget-simulator-built \
  "$DD/Build/Intermediates.noindex/Taptime.build/Debug-iphonesimulator/TaptimeWidgets.build/TaptimeWidgets.appex-Simulated.xcent" \
  "$ALLOWED_WIDGET_BUILT_ENTITLEMENTS"

echo "mobile-ios-test: OK"
