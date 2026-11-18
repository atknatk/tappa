import XCTest

// The test bundle is hosted by the app, so Bundle.main is the BUILT Taptime.app and these
// tests read the Info.plist the build produced, not the source file. That is the point: a
// build setting, an xcconfig or an INFOPLIST_KEY_ entry can add a key the source plist never
// shows (M16-02 audit; the redline R2 scan only reads source text).
//
// The permission keys are checked as an ALLOW-LIST rather than by naming the forbidden ones:
// any usage-description key other than the two the card allows fails, whatever it is called.
final class InfoPlistTests: XCTestCase {
    private var info: [String: Any] {
        Bundle.main.infoDictionary ?? [:]
    }

    func testTheBundleIsTheStaffApp() {
        XCTAssertEqual(Bundle.main.bundleIdentifier, "mt.taptime.app")
        XCTAssertEqual(info["CFBundleDisplayName"] as? String, "Taptime")
    }

    // iPhone 7's last iOS is 15.x (CoreSimulator's iPhone 7 profile: maxRuntimeVersion
    // 15.255.255), so the app must run on 15.0. SwiftUI's App (iOS 14) and
    // CLLocation.sourceInformation (iOS 15) are both inside it.
    func testTheDeploymentTargetCoversIPhone7() {
        XCTAssertEqual(info["MinimumOSVersion"] as? String, "15.0")
    }

    func testOnlyTheTwoAllowedUsageDescriptions() {
        let usage = Set(info.keys.filter { $0.hasSuffix("UsageDescription") })
        XCTAssertEqual(usage, ["NFCReaderUsageDescription", "NSLocationWhenInUseUsageDescription"])
        for key in ["NFCReaderUsageDescription", "NSLocationWhenInUseUsageDescription"] {
            let text = info[key] as? String ?? ""
            XCTAssertFalse(text.isEmpty, "\(key) has no text")
        }
    }

    // CLAUDE.md 4.2: location only at the moment of the tap. No background mode at all (the
    // app has no reason to run in the background), and no key of the Always family.
    func testNoBackgroundModeAndNoAlwaysLocation() {
        XCTAssertNil(info["UIBackgroundModes"])
        XCTAssertEqual(info.keys.filter { $0.hasPrefix("NSLocation") }, ["NSLocationWhenInUseUsageDescription"])
    }
}
