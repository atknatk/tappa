import CoreLocation
import CoreText
import CryptoKit
import XCTest
@testable import Taptime

/// The tap end to end against the surface's twin (Support.swift), through URLProtocol: the two
/// ways in (a plaque link on iPhone XS+, the app's own button on iPhone 7-X), the press, the
/// result, and every screen a failure lands on. The twin verifies every signature it is sent.
@MainActor
final class TapModelTests: XCTestCase {
    let local = Date(timeIntervalSince1970: 1_794_700_800)
    let plaque = URL(string: "https://taptime.mt/t?tag=04A1B2C3D4E5F6&ctr=000012&cmac=8899AABBCCDDEEFF")!
    let sun = "tag=04A1B2C3D4E5F6&ctr=000012&cmac=8899AABBCCDDEEFF"
    var server: FakeAppServer!
    var store: MemoryActivationStore!
    var reader: FakePlaqueReader!
    var location: FakeLocation!
    var browser: FakeBrowser!
    var activation: ActivationModel!
    var tap: TapModel!
    var app: AppModel!
    var deviceID = ""
    /// Requests that failed in transport before the twin saw them (see offline(paths:)).
    var lost: [String] = []

    override func setUp() async throws {
        // The phone's clock is the server's here, so every request below is answered on its first
        // try and the paths list exactly what the flow sends (the skew retry is AppClientTests').
        server = FakeAppServer(now: local)
        let s = server!
        MockURLProtocol.handler = { req, body in try s.handle(req, body) }
        store = MemoryActivationStore()
        reader = FakePlaqueReader()
        location = FakeLocation()
        browser = FakeBrowser()
        let l = local
        let client = AppClient(baseURL: URL(string: "https://taptime.mt")!,
                               session: AppClient.makeSession(protocolClasses: [MockURLProtocol.self]),
                               clock: ServerClock(localNow: { l }))
        activation = ActivationModel(client: client, keys: SoftwareKeyFactory(), store: store, reader: reader)
        tap = TapModel(client: client, reader: reader, location: location, browser: browser, now: { l })
        app = AppModel(activation: activation, tap: tap, browser: browser)
        let k = SoftwareDeviceKey()
        deviceID = server.register(k)
        store.record = ActivationRecord(deviceId: deviceID, employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        lost = []
    }

    override func tearDown() async throws {
        MockURLProtocol.handler = nil
    }

    /// Requests to these paths never reach the twin (offline); everything else does.
    private func offline(paths: Set<String>) {
        let s = server!
        MockURLProtocol.handler = { [weak self] req, body in
            let p = req.url?.path ?? ""
            if paths.contains(p) {
                self?.lost.append(p)
                throw URLError(.notConnectedToInternet)
            }
            return try s.handle(req, body)
        }
    }

    private var view: TapModel.View? {
        if case .tap(let v, _) = tap.screen {
            return v
        }
        return nil
    }

    private func waitFor(_ done: () -> Bool) async throws {
        for _ in 0..<300 where !done() {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
    }

    // MARK: iPhone 7-X (and any iPhone opened from its home screen)

    /// Q41 (a): the app opens on the tap screen with /me's name and predicted direction; no venue
    /// yet, and nothing read or recorded.
    func testTheAppOpensOnTheTapScreenOfMe() async {
        await app.launch()
        XCTAssertEqual(activation.screen, .activated)
        XCTAssertEqual(view, TapModel.View(employeeName: "Maria Borg", locationName: "", brand: .taptime,
                                           direction: "in", source: .home))
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/me"])
        XCTAssertTrue(reader.alerts.isEmpty)
        XCTAssertTrue(location.reads.isEmpty, "§4.2: no reading before a press")
    }

    /// One press: the scan sheet, the read's tap/start, then the press with the direction the
    /// button said and the one fix -- channel nfc, a real SUN read, on the server.
    func testOneButtonReadsThePlaqueAndRecordsTheTap() async throws {
        await app.launch()
        let pressing = Task { await tap.press(want: "in") }
        try await waitFor { reader.pending != nil }
        XCTAssertEqual(reader.alerts, [TapCopy.Screen.sheet])
        XCTAssertEqual(location.reads, [local], "the one reading starts at the press")
        reader.deliver(.url(plaque))
        await pressing.value

        XCTAssertEqual(server.tapStarts, [sun])
        XCTAssertEqual(server.taps.count, 1)
        let body = try XCTUnwrap(server.taps.first)
        XCTAssertEqual(body["context"] as? String, "signed-context-1")
        XCTAssertEqual(body["want"] as? String, "in")
        XCTAssertEqual(body["lat"] as? Double, 35.9189)
        XCTAssertEqual(body["lng"] as? Double, 14.4893)
        XCTAssertNil(body["mockLocation"], "a reading not reported as simulated sends no flag at all")
        XCTAssertEqual(tap.screen, .result(TapModel.Result(verdict: "ok", direction: "in", venue: "St Julians",
                                                           at: "09:12", businessType: "restaurant", practice: false)))
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/me", "/app/v1/tap/start", "/app/v1/nonce", "/app/v1/tap"])
        XCTAssertEqual(location.reads.count, 1)
        XCTAssertFalse(tap.pressing)
    }

    func testTheDirectionNeutralTapSendsNoWantAndNoFixSendsNoCoordinates() async throws {
        server.meBody["direction"] = ""
        location.answer = .none
        await app.launch()
        XCTAssertEqual(view?.direction, "")
        let pressing = Task { await tap.press(want: nil) }
        try await waitFor { reader.pending != nil }
        reader.deliver(.url(plaque))
        await pressing.value
        XCTAssertEqual(server.taps.first?.keys.sorted(), ["context"], "absent, never null")
    }

    func testAMockReportGoesWithTheFix() async throws {
        location.answer = TapLocationReport(lat: 35.9, lng: 14.5, mock: true)
        await app.launch()
        await app.open(plaque)
        await tap.press(want: "in")
        XCTAssertEqual(server.taps.first?["mockLocation"] as? Bool, true)
    }

    /// The flag is never sent as false, and never without the coordinates it describes: every value
    /// sent lands in the immutable transactions row.
    func testTheMockFlagIsOnlyEverTrueAndOnlyWithAFix() async throws {
        await app.launch()
        for answer in [TapLocationReport(lat: 35.9, lng: 14.5, mock: false),
                       TapLocationReport(lat: nil, lng: nil, mock: true),
                       TapLocationReport(lat: nil, lng: nil, mock: false)] {
            location.answer = answer
            await app.open(plaque)
            await tap.press(want: "in")
            let body = try XCTUnwrap(server.taps.last)
            XCTAssertNil(body["mockLocation"], "\(answer)")
        }
    }

    func testAClosedSheetStaysAndAFailedReadSaysSoAndAnotherHostIsNeverSent() async throws {
        await app.launch()
        var pressing = Task { await tap.press(want: "in") }
        try await waitFor { reader.pending != nil }
        reader.deliver(.cancelled)
        await pressing.value
        XCTAssertEqual(tap.screen, .tap(view!, problem: nil))
        XCTAssertFalse(tap.pressing)

        pressing = Task { await tap.press(want: "in") }
        try await waitFor { reader.pending != nil }
        reader.deliver(.failed)
        await pressing.value
        XCTAssertEqual(tap.screen, .tap(view!, problem: TapCopy.Problems.notRead))

        pressing = Task { await tap.press(want: "in") }
        try await waitFor { reader.pending != nil }
        reader.deliver(.url(URL(string: "https://evil.example/t?tag=04&ctr=000001&cmac=00")!))
        await pressing.value
        XCTAssertEqual(tap.screen, .tap(view!, problem: TapCopy.Problems.notRead))
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/me"])
    }

    /// M16-15 hand-off 1: without a reader (iPhone 6s/SE run iOS 15) no session is ever opened.
    func testWithoutAReaderNoSessionIsOpened() async {
        reader.isAvailable = false
        await app.launch()
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.noReader, retry: false)))
        XCTAssertTrue(reader.alerts.isEmpty)
        // A plaque link still works there: the press needs no reader.
        await app.open(plaque)
        await tap.press(want: "in")
        XCTAssertEqual(server.taps.count, 1)
        XCTAssertTrue(reader.alerts.isEmpty)
    }

    // MARK: iPhone XS and later: the plaque link

    func testAPlaqueLinkShowsThatReadsScreenAndThePressSendsIt() async {
        await app.launch()
        await app.open(plaque)
        XCTAssertEqual(view, TapModel.View(employeeName: "Maria Borg", locationName: "St Julians",
                                           brand: AppBrand(name: nil, logo: nil, accent: "DA291C"), direction: "in",
                                           source: .plaque(sun: sun, context: "signed-context-1")))
        XCTAssertTrue(server.taps.isEmpty, "the screen records nothing")
        XCTAssertTrue(location.reads.isEmpty)
        await tap.press(want: "out")
        XCTAssertTrue(reader.alerts.isEmpty, "the link was the read: no sheet")
        XCTAssertEqual(server.tapStarts, [sun])
        XCTAssertEqual(server.taps.first?["want"] as? String, "out", "what was pressed")
        guard case .result = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
    }

    /// A link that LAUNCHED the app waits for the record and goes straight to its screen.
    func testALinkThatLaunchedTheAppIsRoutedOnceTheRecordIsRead() async {
        await app.open(plaque)
        XCTAssertTrue(server.seen.isEmpty)
        await app.launch()
        XCTAssertEqual(view?.locationName, "St Julians")
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/tap/start"])
    }

    /// The press waits for its one fix, as tap.js does, and then sends it.
    func testThePressWaitsForItsFix() async throws {
        location.hold = true
        await app.launch()
        await app.open(plaque)
        let pressing = Task { await tap.press(want: "in") }
        try await waitFor { location.reads.count == 1 }
        XCTAssertTrue(tap.pressing)
        XCTAssertTrue(server.taps.isEmpty)
        location.deliver()
        await pressing.value
        XCTAssertEqual(server.taps.first?["lat"] as? Double, 35.9189)
    }

    // MARK: Back in front

    /// The result has no button (§9); an iPhone 7-X could not go on from it, so the app comes back
    /// on its tap screen.
    func testBackInFrontTheResultGivesWayToTheTapScreen() async throws {
        await app.launch()
        await app.open(plaque)
        await tap.press(want: "in")
        app.scenePhase(.background)
        app.scenePhase(.active)
        try await waitFor { view?.source == .home }
        XCTAssertEqual(view?.source, .home)
        XCTAssertEqual(server.seen.filter { $0.path == "/app/v1/me" }.count, 2)
    }

    /// A result that came back while the app was away is shown when it returns -- not replaced by
    /// the home screen unseen. The next return goes home as usual.
    func testAResultThatArrivedInTheBackgroundIsShownFirst() async throws {
        location.hold = true
        await app.launch()
        await app.open(plaque)
        let pressing = Task { await tap.press(want: "in") }
        try await waitFor { location.reads.count == 1 }
        app.scenePhase(.background)
        location.deliver()
        await pressing.value
        guard case .result = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
        app.scenePhase(.active)
        try await Task.sleep(nanoseconds: 50_000_000)
        guard case .result = tap.screen else {
            return XCTFail("the unseen result was replaced: \(tap.screen)")
        }
        XCTAssertEqual(server.seen.filter { $0.path == "/app/v1/me" }.count, 1)
        app.scenePhase(.background)
        app.scenePhase(.active)
        try await waitFor { view?.source == .home }
        XCTAssertEqual(view?.source, .home, "seen once, the next return goes home")
    }

    /// A plaque link wins over an unseen result too.
    func testALinkWinsOverAnUnseenResult() async throws {
        location.hold = true
        await app.launch()
        await app.open(plaque)
        let pressing = Task { await tap.press(want: "in") }
        try await waitFor { location.reads.count == 1 }
        app.scenePhase(.background)
        location.deliver()
        await pressing.value
        app.linkArrived()
        await app.open(plaque)
        app.scenePhase(.active)
        XCTAssertEqual(view?.locationName, "St Julians")
    }

    /// A plaque link that brought the app to the front wins over the way back home.
    func testALinkThatBroughtTheAppBackWins() async throws {
        await app.launch()
        app.scenePhase(.background)
        app.linkArrived()
        await app.open(plaque)
        app.scenePhase(.active)
        try await Task.sleep(nanoseconds: 50_000_000)
        XCTAssertEqual(view?.locationName, "St Julians")
    }

    /// iOS delivers .active FIRST and the link after it (RootView: linkArrived synchronously, then a
    /// Task for open). The way home is already queued as a Task when the link arrives; when it
    /// runs -- here while open's tap/start is in flight -- it must stand aside, or its generation
    /// would drop the plaque's answer.
    func testALinkAfterActiveStillWins() async throws {
        await app.launch()
        app.scenePhase(.background)
        app.scenePhase(.active)   // queues the way home
        app.linkArrived()         // onOpenURL, synchronously
        await app.open(plaque)    // suspends at tap/start; the queued way home runs meanwhile
        try await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(view?.locationName, "St Julians")
        XCTAssertEqual(server.seen.filter { $0.path == "/app/v1/me" }.count, 1, "only the launch read /me")
    }

    // MARK: Problems

    /// Two 401s on the home read: the activation screens, "This phone was signed out".
    func testSignedOutAtHomeIsTheActivationScreen() async {
        server.devicesSignedOut.insert(deviceID)
        await app.launch()
        XCTAssertEqual(activation.screen, .code(notice: Copy.Problems.signedOut))
        XCTAssertNotNil(store.record)
    }

    /// Two 401s on a plaque: the same words, and Safari for the same, unspent URL.
    func testSignedOutOnAPlaqueOffersTheSameURLInSafari() async {
        await app.launch()
        server.devicesSignedOut.insert(deviceID)
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: Copy.Problems.signedOut, retry: false,
                                                             browserURL: plaque, startAgain: true)))
        tap.openInBrowser(plaque)
        XCTAssertEqual(browser.opened, [plaque])
        tap.startAgain()
        XCTAssertEqual(activation.screen, .code(notice: nil))
        XCTAssertTrue(server.taps.isEmpty)
    }

    /// "Open in browser" opens the URL REBUILT from the server's origin and the checked query, not
    /// the link as it arrived (here: an upper-case host, which the check accepts).
    func testSafariGetsTheRebuiltPlaqueURL() async {
        await app.launch()
        server.devicesSignedOut.insert(deviceID)
        await app.open(URL(string: "https://TAPTIME.MT/t?" + sun)!)
        guard case .problem(let p) = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
        XCTAssertEqual(p.browserURL, plaque)
    }

    /// M16-16 hand-off 4: a phone clock days off is never measured, so every read is a 401 -- the
    /// screen says to check the clock, not "signed out".
    func testAClockDaysOffSaysCheckTheClock() async {
        server.now = local.addingTimeInterval(3 * 86_400)
        await app.launch()
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.clock)))
        XCTAssertEqual(activation.screen, .activated)
    }

    func testOfflineBeforeThePressOffersSafari() async {
        await app.launch()
        offline(paths: [AppClient.Path.tapStart])
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.offline, browserURL: plaque)))
    }

    /// §4.6: an answer lost after the press may belong to a recorded tap. It is sent ONCE, not
    /// retried, and Safari is not offered for a URL that may be spent.
    func testAnAnswerLostAfterThePressIsNotRetried() async {
        await app.launch()
        await app.open(plaque)
        offline(paths: [AppClient.Path.tap])
        await tap.press(want: "in")
        XCTAssertEqual(lost, [AppClient.Path.tap])
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.offlineUnknown)))
    }

    /// After the press, an answer outside the contract (a proxy's 502/504, a 200 that does not
    /// decode) may hide a recorded tap: "may not have been recorded", never "Nothing was recorded".
    /// The surface's own server_error still says nothing was recorded.
    func testAnAnswerOutsideTheContractAfterThePressMayHaveRecorded() async {
        await app.launch()
        for (status, code) in [(502, nil), (504, nil), (200, nil)] as [(Int, String?)] {
            await app.open(plaque)
            server.tapScript = [(status, code)]
            await tap.press(want: "in")
            XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.offlineUnknown)), "\(status)")
        }
        await app.open(plaque)
        server.tapScript = [(500, "server_error")]
        await tap.press(want: "in")
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.server)))
        // Before the press nothing could have been recorded: a 502 on tap/start is the server screen.
        server.tapStartScript = [(502, nil)]
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.server)))
    }

    /// The surface's refusals land on the browser's own screens; "Try again" goes home.
    func testRefusalsAreTheBrowsersScreens() async throws {
        let table: [(Int, String, Copy.Problem)] = [
            (400, "stale_context", TapCopy.Problems.stale),
            (403, "foreign_tenant", TapCopy.Problems.foreignTenant),
            (404, "unknown_plaque", TapCopy.Problems.unknownPlaque),
            (429, "rate_limited", TapCopy.Problems.tooMany),
            (409, "try_again", TapCopy.Problems.server),
            (500, "server_error", TapCopy.Problems.server),
        ]
        await app.launch()
        for (status, code, want) in table {
            await app.open(plaque)
            server.tapScript = [(status, code)]
            await tap.press(want: "in")
            XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: want)), code)
        }
        server.tapStartScript = [(400, "bad_request")]
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.badURL)))
        tap.retry()
        try await waitFor { view?.source == .home }
        XCTAssertEqual(view?.source, .home)
    }

    func testOfflineAtHomeKeepsTheStoredNameAndTheNeutralTap() async {
        offline(paths: [AppClient.Path.me])
        await app.launch()
        XCTAssertEqual(view, TapModel.View(employeeName: "Maria Borg", locationName: "", brand: .taptime,
                                           direction: "", source: .home))
    }
}

/// The words are the browser's (TapCopy.swift says where each comes from).
final class TapCopyTests: XCTestCase {
    func testTheButtonAndItsCorrection() {
        XCTAssertEqual(TapCopy.button("in"), "Clock in")
        XCTAssertEqual(TapCopy.button("out"), "Clock out")
        XCTAssertEqual(TapCopy.button(""), "Tap")
        XCTAssertEqual(TapCopy.correction("in"), "Not right? Clock out")
        XCTAssertEqual(TapCopy.correction("out"), "Not right? Clock in")
        XCTAssertEqual(TapCopy.correction(""), "")
        XCTAssertEqual(TapCopy.other("in"), "out")
    }

    /// result.templ / view.go, verdict by verdict.
    func testTheResultSaysWhatTheBrowserSays() {
        XCTAssertEqual(TapCopy.Result.headline(verdict: "ok", direction: "in"), "Clocked in")
        XCTAssertEqual(TapCopy.Result.headline(verdict: "flag", direction: "out"), "Clocked out")
        XCTAssertEqual(TapCopy.Result.headline(verdict: "ok", direction: ""), "Tapped")
        XCTAssertEqual(TapCopy.Result.headline(verdict: "ignored", direction: "in"), "Already tapped")
        XCTAssertEqual(TapCopy.Result.headline(verdict: "reject", direction: "in"), "Not counted")
        XCTAssertEqual(TapCopy.Result.headline(verdict: "new", direction: "in"), "Tapped")
        XCTAssertEqual(TapCopy.Result.closing(verdict: "ok"), ["All done — you can close the app."])
        XCTAssertEqual(TapCopy.Result.closing(verdict: "flag"), ["You can close the app."])
        XCTAssertEqual(TapCopy.Result.closing(verdict: "ignored").count, 2)
        XCTAssertEqual(TapCopy.Result.closing(verdict: "reject").first, TapCopy.Result.rejected)
        XCTAssertEqual(TapCopy.Result.brandLine(verdict: "ok", direction: "in", businessType: "restaurant", practice: false),
                       "Have a great shift — keep those kebabs rolling! 🌯")
        XCTAssertNil(TapCopy.Result.brandLine(verdict: "flag", direction: "in", businessType: "restaurant", practice: false))
        XCTAssertNil(TapCopy.Result.brandLine(verdict: "ok", direction: "in", businessType: "restaurant", practice: true))
        XCTAssertEqual(TapCopy.Result.brandLine(verdict: "ok", direction: "", businessType: "x", practice: false),
                       "Thanks — have a good one!")
    }

    /// The tap surface's problem screens, word for word where the browser has them
    /// (internal/handler/tap.go), read from the Go source so a change there turns this red.
    func testTheProblemsAreTheTapSurfacesOwn() throws {
        let go = try String(contentsOf: URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("internal/handler/tap.go"), encoding: .utf8)
        for p in [TapCopy.Problems.unknownPlaque, TapCopy.Problems.server, TapCopy.Problems.tooMany,
                  TapCopy.Problems.foreignTenant] {
            for line in [p.title, p.message, p.hint] {
                XCTAssertTrue(go.contains("\"\(line)\""), line)
            }
        }
        for line in [TapCopy.Problems.badURL.title, TapCopy.Problems.badURL.hint, TapCopy.Problems.stale.title,
                     TapCopy.Problems.stale.hint] {
            XCTAssertTrue(go.contains("\"\(line)\""), line)
        }
    }
}

/// The location reading's rules, on readings made here (no Core Location run).
final class OneShotLocationTests: XCTestCase {
    let press = Date(timeIntervalSince1970: 1_794_700_800)

    private func reading(age: TimeInterval, accuracy: Double = 10) -> CLLocation {
        CLLocation(coordinate: CLLocationCoordinate2D(latitude: 35.9189, longitude: 14.4893), altitude: 0,
                   horizontalAccuracy: accuracy, verticalAccuracy: -1, timestamp: press.addingTimeInterval(-age))
    }

    func testAFreshPreciseReadingIsSent() {
        let r = OneShotLocation.report(reading(age: -2), pressedAt: press, precise: true)
        XCTAssertEqual(r.lat, 35.9189)
        XCTAssertEqual(r.lng, 14.4893)
    }

    /// tap.js's maximumAge 0: a reading from before the press is where the phone WAS.
    func testAReadingOlderThanThePressIsNotSent() {
        XCTAssertNil(OneShotLocation.report(reading(age: 30), pressedAt: press, precise: true).lat)
    }

    /// Precise Location off: a fix fuzzed by kilometres is not evidence of presence.
    func testAnApproximateReadingIsNotSent() {
        XCTAssertNil(OneShotLocation.report(reading(age: -2), pressedAt: press, precise: false).lat)
    }

    func testAnInvalidReadingIsNotSent() {
        XCTAssertNil(OneShotLocation.report(reading(age: -2, accuracy: -1), pressedAt: press, precise: true).lat)
    }

    private func reading(simulated: Bool, age: TimeInterval = -2, precise: Bool = true) -> TapLocationReport {
        let info = CLLocationSourceInformation(softwareSimulationState: simulated, andExternalAccessoryState: false)
        let r = CLLocation(coordinate: CLLocationCoordinate2D(latitude: 35.9189, longitude: 14.4893), altitude: 0,
                           horizontalAccuracy: 10, verticalAccuracy: -1, course: -1, courseAccuracy: -1, speed: -1,
                           speedAccuracy: -1, timestamp: press.addingTimeInterval(-age), sourceInfo: info)
        return OneShotLocation.report(r, pressedAt: press, precise: precise)
    }

    /// isSimulatedBySoftware: true → mock true with the fix; false → no flag at all (never false);
    /// a simulated reading that is not sent (old, approximate) → no flag either.
    func testTheMockReportIsTrueOrAbsent() {
        let sim = reading(simulated: true)
        XCTAssertEqual(sim.mock, true)
        XCTAssertNotNil(sim.lat)
        XCTAssertNil(reading(simulated: false).mock)
        XCTAssertNil(reading(simulated: true, age: 30).mock)
        XCTAssertNil(reading(simulated: true, precise: false).mock)
    }
}

/// §4.2 and the card's call-site allow-list: the one location call, the one permission request and
/// the one location manager of the app are in the tap flow's file, and nothing anywhere starts
/// location in any other way. The forbidden names are assembled from halves so that this file does
/// not itself carry them (redline R2 scans tests too).
final class TapSourceGuardTests: XCTestCase {
    private func appSources() throws -> [(String, String)] {
        let dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().appendingPathComponent("Taptime")
        let names = try FileManager.default.contentsOfDirectory(atPath: dir.path).filter { $0.hasSuffix(".swift") }
        XCTAssertFalse(names.isEmpty)
        return try names.map { ($0, try String(contentsOf: dir.appendingPathComponent($0), encoding: .utf8)) }
    }

    private func count(_ needle: String, in s: String) -> Int {
        s.components(separatedBy: needle).count - 1
    }

    func testTheOneLocationCallIsInTheTapFlow() throws {
        let allowed: [(String, Int)] = [
            ("request" + "Location(", 2), // the press, and the press whose permission was just given
            ("request" + "WhenInUseAuthorization(", 1),
            ("CLLocation" + "Manager()", 1),
        ]
        for (needle, n) in allowed {
            var total = 0
            for (name, src) in try appSources() {
                let c = count(needle, in: src)
                total += c
                if c > 0 {
                    XCTAssertEqual(name, "TapFlow.swift", needle)
                }
            }
            XCTAssertEqual(total, n, needle)
        }
    }

    func testNothingStartsLocationAnyOtherWay() throws {
        let forbidden = [
            "start" + "UpdatingLocation", "request" + "AlwaysAuthorization", "start" + "Monitoring",
            "CLLocation" + "Update", "CL" + "Monitor", "allowsBackground" + "LocationUpdates",
            "CLBackground" + "ActivitySession", "live" + "Updates", "CLCircular" + "Region",
        ]
        for (name, src) in try appSources() {
            for f in forbidden {
                XCTAssertEqual(count(f, in: src), 0, "\(name): \(f)")
            }
        }
    }

    /// The card's trap: no repeating timer beside the call -- and none on the tap screens at all.
    func testNoTimerOnTheTapFlowOrItsScreens() throws {
        for (name, src) in try appSources() where name == "TapFlow.swift" || name == "TapViews.swift" {
            for f in ["Tim" + "er", "repeats" + ":", "while " + "true"] {
                XCTAssertEqual(count(f, in: src), 0, "\(name): \(f)")
            }
        }
    }

    /// CLAUDE.md §7: the files that hold a context, a nonce, a signature or a coordinate log nothing.
    func testTheTapFilesLogNothing() throws {
        for (name, src) in try appSources() where ["TapFlow.swift", "AppClient.swift", "AppModel.swift"].contains(name) {
            for f in ["pri" + "nt(", "NS" + "Log", "os" + "_log", "Log" + "ger", "dump" + "("] {
                XCTAssertEqual(count(f, in: src), 0, "\(name): \(f)")
            }
        }
    }
}

/// The two typefaces come from web/static/fonts, unchanged, and draw the Maltese letters
/// themselves (M16-15 hand-off 2).
final class TypefaceTests: XCTestCase {
    func testTheBundledFilesAreTheWebsOwnBytes() throws {
        let fonts = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().appendingPathComponent("web/static/fonts")
        for name in Typeface.File.all {
            let bundled = try XCTUnwrap(Bundle.main.url(forResource: name, withExtension: "woff2"), name)
            let repo = fonts.appendingPathComponent(name + ".woff2")
            XCTAssertEqual(SHA256.hash(data: try Data(contentsOf: bundled)), SHA256.hash(data: try Data(contentsOf: repo)), name)
        }
        for licence in ["OFL-SpaceGrotesk", "OFL-IBMPlexMono"] {
            XCTAssertNotNil(Bundle.main.url(forResource: licence, withExtension: "txt"), "the OFL travels with the font")
        }
    }

    private func runFonts(_ font: CTFont, _ text: String) -> [String] {
        let a = NSAttributedString(string: text, attributes: [NSAttributedString.Key(kCTFontAttributeName as String): font])
        let line = CTLineCreateWithAttributedString(a)
        return (CTLineGetGlyphRuns(line) as? [CTRun] ?? []).compactMap { run in
            let attrs = CTRunGetAttributes(run) as NSDictionary
            guard let f = attrs[kCTFontAttributeName as String] else { return nil }
            return CTFontCopyPostScriptName(f as! CTFont) as String
        }
    }

    func testTheFacesLoadAndDrawMalteseNames() throws {
        let faces = Typeface.Faces(bundle: .main)
        let display = try XCTUnwrap(faces.display(20, .bold))
        let mono = try XCTUnwrap(faces.mono(15, bold: false))
        let monoBold = try XCTUnwrap(faces.mono(15, bold: true))
        let d = runFonts(display, "Hello Ċikku Żammit ħ ġ")
        XCTAssertFalse(d.isEmpty)
        XCTAssertTrue(d.allSatisfy { $0.hasPrefix("SpaceGrotesk") }, "\(d)")
        let m = runFonts(mono, "09:12 Ż")
        XCTAssertTrue(m.allSatisfy { $0 == "IBMPlexMono-Regular" }, "\(m)")
        XCTAssertTrue(runFonts(monoBold, "01").allSatisfy { $0 == "IBMPlexMono-Bold" })
        // The weight axis is set: bold is wider than regular for the same text.
        let regular = try XCTUnwrap(faces.display(20, .regular))
        func width(_ f: CTFont) -> Double {
            let a = NSAttributedString(string: "Clock in", attributes: [NSAttributedString.Key(kCTFontAttributeName as String): f])
            return CTLineGetTypographicBounds(CTLineCreateWithAttributedString(a), nil, nil, nil)
        }
        XCTAssertGreaterThan(width(display), width(regular))
    }
}

final class ServerClockFarOffTests: XCTestCase {
    func testAFarOffAnswerIsRememberedUntilAnInRangeOne() {
        let local = Date(timeIntervalSince1970: 1_794_700_800)
        let c = ServerClock(localNow: { local })
        XCTAssertFalse(c.lastAnswerWasFarOff)
        XCTAssertFalse(c.observe(dateHeader: httpDate(local.addingTimeInterval(2 * 86_400)), sentAt: local, receivedAt: local))
        XCTAssertTrue(c.lastAnswerWasFarOff)
        XCTAssertFalse(c.observe(dateHeader: "garbage", sentAt: local, receivedAt: local))
        XCTAssertTrue(c.lastAnswerWasFarOff, "an unreadable header says nothing")
        XCTAssertTrue(c.observe(dateHeader: httpDate(local.addingTimeInterval(30)), sentAt: local, receivedAt: local))
        XCTAssertFalse(c.lastAnswerWasFarOff)
    }
}

/// The plaque URL's gate (M16-17 review): the chip writes https://<host>/t?<query> and nothing
/// else, and what Safari is handed is rebuilt from the server's origin.
final class PlaqueURLTests: XCTestCase {
    let host = "taptime.mt"
    let q = "tag=04A1B2C3D4E5F6&ctr=000012&cmac=8899AABBCCDDEEFF"

    func testOnlyTheChipsShapePasses() {
        XCTAssertEqual(PlaqueURL.sunQuery(of: URL(string: "https://taptime.mt/t?" + q)!, host: host), q)
        for bad in ["https://taptime.mt:8443/t?", "http://taptime.mt/t?", "https://taptime.mt/t/?", "https://evil.example/t?"] {
            XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: bad + q)!, host: host), bad)
        }
        // A user, with and without a password -- built from components, so no credentialed URL is
        // spelled in the source (redline R7d reads tests too).
        for (user, password) in [("u", nil), ("u", "p")] as [(String, String?)] {
            var c = URLComponents(string: "https://taptime.mt/t?" + q)!
            c.user = user
            c.password = password
            XCTAssertNil(PlaqueURL.sunQuery(of: c.url!, host: host), user)
        }
        XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: "https://taptime.mt/t?" + q + "#x")!, host: host))
        XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: "https://taptime.mt/t")!, host: host))
    }

    func testTheBrowserURLIsRebuilt() {
        XCTAssertEqual(PlaqueURL.url(base: URL(string: "https://taptime.mt")!, sun: q)?.absoluteString,
                       "https://taptime.mt/t?" + q)
    }
}
