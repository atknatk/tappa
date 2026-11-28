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

    // M16-22 (Q36 (c)): a venue screen's live QR is read by the SYSTEM camera and opens the app as a
    // Universal Link; the app has no QR reader, so it asks for no camera. The allow-list above already
    // refuses the key; this names it.
    func testNoCameraUsageDescription() {
        XCTAssertNil(info["NSCameraUsageDescription"])
    }

    // CLAUDE.md 4.2: location only at the moment of the tap. No background mode at all (the
    // app has no reason to run in the background), and no key of the Always family.
    func testNoBackgroundModeAndNoAlwaysLocation() {
        XCTAssertNil(info["UIBackgroundModes"])
        XCTAssertEqual(info.keys.filter { $0.hasPrefix("NSLocation") }, ["NSLocationWhenInUseUsageDescription"])
    }

    // M16-47 [G47-c]: the lock-screen counter is a Live Activity the app starts itself. The key is
    // the only addition; no usage description comes with it (the allow-list above still holds).
    func testLiveActivitiesAreDeclared() {
        XCTAssertEqual(info["NSSupportsLiveActivities"] as? Bool, true)
    }

    // M16-47 [G47-c]: the one embedded extension is the widget, its Info.plist is the WidgetKit
    // extension point and nothing else -- no background mode, no usage description -- and it runs
    // where the app runs (15.0; its Live Activity is behind iOS 16.2 in code).
    func testTheOnlyExtensionIsTheWidget() throws {
        let plugins = try XCTUnwrap(Bundle.main.builtInPlugInsURL)
        // The test bundle itself is injected there too (it is not shipped); the app's own are the .appex.
        let found = try FileManager.default.contentsOfDirectory(atPath: plugins.path).filter { $0.hasSuffix(".appex") }.sorted()
        XCTAssertEqual(found, ["TaptimeWidgets.appex"])
        let ext = try XCTUnwrap(Bundle(url: plugins.appendingPathComponent("TaptimeWidgets.appex")))
        let plist = ext.infoDictionary ?? [:]
        XCTAssertEqual(ext.bundleIdentifier, "mt.taptime.app.widgets")
        XCTAssertEqual(plist["MinimumOSVersion"] as? String, "15.0")
        let nsext = try XCTUnwrap(plist["NSExtension"] as? [String: Any])
        XCTAssertEqual(Set(nsext.keys), ["NSExtensionPointIdentifier"])
        XCTAssertEqual(nsext["NSExtensionPointIdentifier"] as? String, "com.apple.widgetkit-extension")
        XCTAssertNil(plist["UIBackgroundModes"])
        XCTAssertTrue(plist.keys.filter { $0.hasSuffix("UsageDescription") }.isEmpty)
        XCTAssertTrue(plist.keys.filter { $0.hasPrefix("NSLocation") }.isEmpty)
    }

    // App Store validation refuses an extension whose version or build is not its app's. Both
    // targets take MARKETING_VERSION and CURRENT_PROJECT_VERSION from the PROJECT's build
    // settings (none of their own), so a bump moves both; this holds the built bundles to it.
    func testTheWidgetCarriesTheAppsVersions() throws {
        let plugins = try XCTUnwrap(Bundle.main.builtInPlugInsURL)
        let ext = try XCTUnwrap(Bundle(url: plugins.appendingPathComponent("TaptimeWidgets.appex")))
        for key in ["CFBundleShortVersionString", "CFBundleVersion"] {
            let app = try XCTUnwrap(info[key] as? String, key)
            XCTAssertFalse(app.isEmpty, key)
            XCTAssertEqual(ext.infoDictionary?[key] as? String, app, key)
        }
    }
}
