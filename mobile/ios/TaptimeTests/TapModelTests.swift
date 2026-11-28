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
    /// The reminder, the widget's share and the lock-screen counter AppModel drives (M16-46/47).
    var reminder: FakeReminder!
    var counter: FakeCounter!
    var shareSuite = ""
    var share: PresenceShareStore!
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
        reminder = FakeReminder()
        counter = FakeCounter()
        shareSuite = "taptime.tests.\(UUID().uuidString)"
        share = PresenceShareStore(defaults: UserDefaults(suiteName: shareSuite)!)
        app = AppModel(activation: activation, tap: tap, browser: browser,
                       presence: PresenceSync(store: share, reload: {}, counter: counter, enabled: { true }, now: { l }),
                       reminder: reminder)
        let k = SoftwareDeviceKey()
        deviceID = server.register(k)
        store.record = ActivationRecord(deviceId: deviceID, employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        lost = []
    }

    override func tearDown() async throws {
        MockURLProtocol.handler = nil
        UserDefaults(suiteName: shareSuite)?.removePersistentDomain(forName: shareSuite)
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
        // The quiet /me after the recorded tap (M16-46/47 trigger 1) is the last request.
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/me", "/app/v1/tap/start", "/app/v1/nonce", "/app/v1/tap", "/app/v1/me"])
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
                                           source: .plaque(link: .plaque(sun: sun), context: "signed-context-1")))
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

    // MARK: Any iPhone: a venue screen's live QR (M16-22)

    /// The camera reads the screen's code and the Universal Link opens the app: tap/start gets the
    /// /q URL's query as `liveQR` (never as `sun`), the screen is the plaque link's -- the name, the
    /// venue, one button -- and the press is the same press. No scan sheet, nothing recorded first.
    func testALiveQRLinkShowsTheTapScreenAndThePressSendsIt() async {
        let q = "t=AQ00112233-_abcXYZ"
        await app.launch()
        await app.open(URL(string: "https://taptime.mt/q?" + q)!)
        XCTAssertEqual(view, TapModel.View(employeeName: "Maria Borg", locationName: "St Julians",
                                           brand: AppBrand(name: nil, logo: nil, accent: "DA291C"), direction: "in",
                                           source: .plaque(link: .liveQR(query: q), context: "signed-context-1")))
        XCTAssertEqual(server.liveQRStarts, [q], "the query, verbatim, as liveQR")
        XCTAssertTrue(server.tapStarts.isEmpty, "never sent as a plaque's sun")
        XCTAssertTrue(server.taps.isEmpty, "the screen records nothing")
        await tap.press(want: "in")
        XCTAssertTrue(reader.alerts.isEmpty, "the camera was the read: no sheet")
        XCTAssertEqual(server.taps.first?["want"] as? String, "in")
        guard case .result = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
    }

    /// An iPhone without an NFC reader still takes a live QR: the reader is not asked.
    func testALiveQRNeedsNoReader() async {
        reader.isAvailable = false
        await app.launch()
        await app.open(URL(string: "https://taptime.mt/q?t=abc")!)
        XCTAssertEqual(view?.locationName, "St Julians")
        XCTAssertEqual(server.liveQRStarts, ["t=abc"])
    }

    /// M16-23 (§9, handed over by M16-22): a live QR's problem screens name the screen's code, not
    /// the plaque -- server, too many, stale, foreign; the plaque's path keeps its words.
    func testALiveQRsProblemsAreTheScreensVariantsAndThePlaquesStayThePlaques() async {
        let live = URL(string: "https://taptime.mt/q?t=abc")!
        await app.launch()
        server.tapStartScript = [(500, "server_error"), (429, "rate_limited")]
        await app.open(live)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.serverLive)))
        await app.open(live)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.tooManyLive)))
        await app.open(live)
        server.tapScript = [(400, "stale_context")]
        await tap.press(want: "in")
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.staleLive)))
        await app.open(live)
        server.tapScript = [(403, "foreign_tenant")]
        await tap.press(want: "in")
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.foreignTenantLive)))
        // The plaque's path: unchanged.
        server.tapStartScript = [(500, "server_error")]
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.server)))
        await app.open(plaque)
        server.tapScript = [(403, "foreign_tenant")]
        await tap.press(want: "in")
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.foreignTenant)))
        // The variants keep the plaque's titles and messages (foreign's excepted) and change the hint.
        for (live, plaque) in [(TapCopy.Problems.serverLive, TapCopy.Problems.server),
                               (TapCopy.Problems.tooManyLive, TapCopy.Problems.tooMany),
                               (TapCopy.Problems.staleLive, TapCopy.Problems.stale)] {
            XCTAssertEqual(live.title, plaque.title)
            XCTAssertEqual(live.message, plaque.message)
            XCTAssertNotEqual(live.hint, plaque.hint)
            XCTAssertFalse(live.hint.contains("plaque"), live.hint)
        }
    }

    /// M16-23: the activated iPhone without a reader is told to use the venue screen's code.
    func testTheNoReaderScreenSendsThePersonToTheVenueScreen() async {
        reader.isAvailable = false
        await app.launch()
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.noReader, retry: false)))
        XCTAssertEqual(TapCopy.Problems.noReader.message,
                       "This iPhone has no NFC reader. To record a tap, open your camera and scan the code on the Taptime screen at your workplace.")
        XCTAssertEqual(TapCopy.Problems.noReader.hint, "No screen there? Ask your manager.")
    }

    /// A live QR URL of another host, port or path is "That link didn't work", sent nowhere.
    func testALiveQRLinkOfAnotherHostIsSentNowhere() async {
        await app.launch()
        let before = server.seen.count
        for bad in ["https://evil.example/q?t=abc", "https://taptime.mt:8443/q?t=abc", "https://taptime.mt/q/?t=abc",
                    "https://taptime.mt/qr?t=abc", "https://taptime.mt/q", "https://taptime.mt/q?t=abc#x"] {
            await app.open(URL(string: bad)!)
            XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.badURL)), bad)
        }
        XCTAssertEqual(server.seen.count, before, "nothing was sent")
    }

    /// Signed out on a live QR: Safari gets the /q URL rebuilt from the server's origin.
    func testSignedOutOnALiveQROffersItsRebuiltURLInSafari() async {
        await app.launch()
        server.devicesSignedOut.insert(deviceID)
        await app.open(URL(string: "https://TAPTIME.MT/q?t=abc")!)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: Copy.Problems.signedOut, retry: false,
                                                             browserURL: URL(string: "https://taptime.mt/q?t=abc")!, startAgain: true)))
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
        // Launch, the quiet one after the recorded tap, and the way home.
        XCTAssertEqual(server.seen.filter { $0.path == "/app/v1/me" }.count, 3)
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
        // Launch, the quiet one after the recorded tap, and the return's catch-up (M16-46 [G46-f]),
        // which leaves the result on screen.
        try await waitFor { server.seen.filter { $0.path == "/app/v1/me" }.count == 3 }
        XCTAssertEqual(server.seen.filter { $0.path == "/app/v1/me" }.count, 3)
        guard case .result = tap.screen else {
            return XCTFail("the catch-up replaced the unseen result: \(tap.screen)")
        }
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

    // MARK: The lock screen, the widget and the reminder follow the tap (M16-46/47 trigger 1)

    /// An open check-in as /me reports it, on the test's clock (the server's is the phone's).
    private var inPresence: [String: Any] {
        ["inSince": "2026-11-15T00:00:00Z", "inSinceLocal": "02:00", "remindAt": "2026-11-15T10:00:00Z",
         "showUntil": "2026-11-15T12:00:00Z", "staleAt": "2026-11-15T13:00:00Z"]
    }

    private var mePaths: Int { server.seen.filter { $0.path == AppClient.Path.me }.count }

    /// A recorded tap: exactly one /me after it and one onMe; the counter starts at clock-in and
    /// ends at clock-out while the app is in front, and the result screen is untouched.
    func testARecordedTapFollowsWithOneQuietMe() async {
        await app.launch()
        XCTAssertEqual(mePaths, 1)
        XCTAssertEqual(reminder.applied.count, 1)
        XCTAssertTrue(counter.live.isEmpty)

        server.meBody["presence"] = inPresence
        server.meBody["direction"] = "out"
        await app.open(plaque)
        await tap.press(want: "in")
        let clockedIn = TapModel.Result(verdict: "ok", direction: "in", venue: "St Julians", at: "09:12",
                                        businessType: "restaurant", practice: false)
        XCTAssertEqual(tap.screen, .result(clockedIn), "the result screen stays exactly as it was")
        XCTAssertEqual(mePaths, 2, "one /me after the tap")
        XCTAssertEqual(server.seen.last?.path, AppClient.Path.me)
        XCTAssertEqual(reminder.applied.count, 2, "one onMe after the tap")
        XCTAssertEqual(tap.me?.presence?.inSinceLocal, "02:00")
        XCTAssertEqual(counter.live.count, 1, "clock-in starts the counter")
        XCTAssertTrue(share.read().isIn)

        server.meBody["presence"] = nil
        server.meBody["direction"] = "in"
        server.tapBody["direction"] = "out"
        await app.open(plaque)
        await tap.press(want: "out")
        XCTAssertEqual(mePaths, 3)
        XCTAssertEqual(reminder.applied.count, 3)
        XCTAssertTrue(counter.live.isEmpty, "clock-out ends it")
        XCTAssertFalse(share.read().isIn)
        guard case .result(let r) = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
        XCTAssertEqual(r.direction, "out")
    }

    /// A tap that records nothing reads no /me: nothing moved.
    func testAFailedPressReadsNoMe() async {
        await app.launch()
        await app.open(plaque)
        server.tapScript = [(500, "server_error")]
        await tap.press(want: "in")
        XCTAssertEqual(mePaths, 1)
        XCTAssertEqual(reminder.applied.count, 1)
    }

    /// The quiet /me unreachable: the last answer stands and the result screen stays.
    func testAnUnreachableQuietMeLeavesTheLastAnswer() async {
        server.meBody["presence"] = inPresence
        await app.launch()
        XCTAssertEqual(counter.live.count, 1)
        await app.open(plaque)
        offline(paths: [AppClient.Path.me])
        await tap.press(want: "out")
        XCTAssertEqual(lost, [AppClient.Path.me])
        XCTAssertEqual(reminder.applied.count, 1)
        XCTAssertEqual(reminder.cancels, 0)
        XCTAssertEqual(counter.live.count, 1)
        XCTAssertTrue(share.read().isIn)
        guard case .result = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
    }

    /// The quiet /me answered signed out: everything is cleared, and the result screen stays.
    func testASignedOutQuietMeClearsAndKeepsTheResult() async {
        server.meBody["presence"] = inPresence
        await app.launch()
        XCTAssertEqual(counter.live.count, 1)
        await app.open(plaque)
        // The press goes through; the server signs the phone out before the /me after it.
        let s = server!
        let id = deviceID
        MockURLProtocol.handler = { req, body in
            if req.url?.path == AppClient.Path.me {
                s.devicesSignedOut.insert(id)
            }
            return try s.handle(req, body)
        }
        await tap.press(want: "out")
        XCTAssertEqual(server.taps.count, 1)
        guard case .result = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
        XCTAssertEqual(activation.screen, .activated, "the result screen is not replaced")
        XCTAssertEqual(reminder.cancels, 1)
        XCTAssertTrue(counter.live.isEmpty)
        XCTAssertEqual(share.read(), .none)
    }

    /// M16-46 [G46-f]: a tap answered while the app was away -- its own follow-up lost there -- is
    /// caught up with on the way back to the front: one quiet /me and one onMe, and the unseen
    /// result stays on screen.
    func testBackInFrontOverAnUnseenResultTheCounterCatchesUp() async throws {
        location.hold = true
        await app.launch()
        await app.open(plaque)
        let pressing = Task { await tap.press(want: "in") }
        try await waitFor { location.reads.count == 1 }
        app.scenePhase(.background)
        offline(paths: [AppClient.Path.me]) // the follow-up from the background does not get through
        location.deliver()
        await pressing.value
        XCTAssertEqual(lost, [AppClient.Path.me])
        XCTAssertTrue(counter.live.isEmpty)
        let s = server!
        MockURLProtocol.handler = { req, body in try s.handle(req, body) }
        server.meBody["presence"] = inPresence
        server.meBody["direction"] = "out"
        let mes = mePaths
        let applied = reminder.applied.count

        app.scenePhase(.active)
        try await waitFor { self.counter.live.count == 1 }
        XCTAssertEqual(mePaths, mes + 1, "one quiet /me")
        XCTAssertEqual(reminder.applied.count, applied + 1, "one onMe")
        XCTAssertEqual(counter.live.count, 1)
        XCTAssertTrue(share.read().isIn)
        XCTAssertEqual(tap.screen, .result(TapModel.Result(verdict: "ok", direction: "in", venue: "St Julians", at: "09:12",
                                                           businessType: "restaurant", practice: false)),
                       "the unseen result is shown, unchanged")
    }

    // MARK: The staff screens' reads answered 401 (the Android twin's StaffFlow.Answer.SignedOut)

    private let openClaim = ClaimsResponse.Open(
        context: "claim-context-1", kind: "out", anchorDate: "2026-11-14", anchorAt: "18:00", day: "yesterday",
        windowFrom: ClaimsResponse.Bound(at: "18:00", inclusive: false), windowTo: ClaimsResponse.Bound(at: "23:59", inclusive: true),
        prefill: nil)

    /// Each staff read -- the Today summary, My hours, the missing times, a claim, the list after
    /// a claim -- takes the home screen's signed-out path: cleared, then "This phone was signed out".
    func testAStaffReadSignedOutIsTheHomeScreensPath() async throws {
        server.meBody["presence"] = inPresence
        server.meBody["hoursWeeks"] = 4
        await app.launch()
        let staff = app.staff
        let claim = openClaim
        let reads: [(name: String, path: String, run: () async -> Void)] = [
            ("Today's summary", AppClient.Path.hours, { await staff.refreshToday() }),
            ("My hours", AppClient.Path.hours, { await staff.loadHours(week: nil, after: nil) }),
            ("the missing times", AppClient.Path.claims, { await staff.openMissing() }),
            ("a claim", AppClient.Path.nonce, { await staff.send(claim, at: "22:00") }),
        ]
        for (i, r) in reads.enumerated() {
            // Activated again, with the counter running.
            server.devicesSignedOut.remove(deviceID)
            guard let d = activation.device else {
                return XCTFail("no device")
            }
            tap.enter(d)
            activation.showActivated()
            await tap.home()
            XCTAssertEqual(counter.live.count, 1, r.name)

            server.devicesSignedOut.insert(deviceID)
            await r.run()
            XCTAssertEqual(server.seen.last?.path, r.path, r.name)
            XCTAssertEqual(activation.screen, .code(notice: Copy.Problems.signedOut), r.name)
            XCTAssertEqual(reminder.cancels, i + 1, r.name)
            XCTAssertTrue(counter.live.isEmpty, r.name)
            XCTAssertEqual(share.read(), .none, r.name)
            XCTAssertNil(staff.claims, r.name)
        }
    }

    /// The list read again after a claim was answered: signed out there too, and no /me after it.
    func testTheListAfterAClaimSignedOutIsTheHomeScreensPath() async throws {
        server.meBody["presence"] = inPresence
        await app.launch()
        let s = server!
        let id = deviceID
        // The claim goes through; the server signs the phone out before the list is read again.
        MockURLProtocol.handler = { req, body in
            if req.url?.path == AppClient.Path.claims {
                s.devicesSignedOut.insert(id)
            }
            return try s.handle(req, body)
        }
        let mes = mePaths
        await app.staff.send(openClaim, at: "22:00")
        XCTAssertEqual(server.seen.last?.path, AppClient.Path.claims)
        XCTAssertEqual(mePaths, mes)
        XCTAssertEqual(activation.screen, .code(notice: Copy.Problems.signedOut))
        XCTAssertEqual(reminder.cancels, 1)
        XCTAssertTrue(counter.live.isEmpty)
    }

    /// A staff read offline is not "signed out", and a clock days off says so instead.
    func testAStaffReadOfflineOrOffClockSignsNothingOut() async throws {
        server.meBody["presence"] = inPresence
        server.meBody["hoursWeeks"] = 4
        await app.launch()
        offline(paths: [AppClient.Path.hours])
        await app.staff.loadHours(week: nil, after: nil)
        XCTAssertTrue(app.staff.hoursFailed)
        XCTAssertEqual(activation.screen, .activated)
        XCTAssertEqual(reminder.cancels, 0)
        XCTAssertEqual(counter.live.count, 1)

        let s = server!
        MockURLProtocol.handler = { req, body in try s.handle(req, body) }
        server.now = local.addingTimeInterval(3 * 86_400)
        await app.staff.openMissing()
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.clock)))
        XCTAssertEqual(activation.screen, .activated)
        XCTAssertEqual(reminder.cancels, 0)
        XCTAssertEqual(counter.live.count, 1)
    }

    // MARK: My hours' pages

    private func hoursPage(_ date: String, next: String?) -> [String: Any] {
        let days: [[String: Any]] = (16...22).map { ["date": "2026-11-\($0)", "workedMinutes": $0 <= 17 ? 480 : 0] }
        let rec: (String, String) -> [String: Any] = { at, dir in
            ["date": date, "at": at, "venue": "V", "direction": dir, "channel": "nfc", "state": "approved"]
        }
        var p: [String: Any] = [
            "week": "2026-11-16", "weeksVisible": 4, "hasPrev": true, "hasNext": false,
            "totals": ["workedMinutes": 960, "awaitingMinutes": 0, "openEntries": 0, "checkInMissing": 0,
                       "truncated": false, "complete": true],
            "days": days, "records": [rec("09:00", "in"), rec("17:00", "out")],
            "waiting": ["records": 0, "claims": 0],
        ]
        if let n = next {
            p["nextCursor"] = n
        }
        return p
    }

    private func twoPages() {
        server.meBody["hoursWeeks"] = 4
        server.hoursPages = ["": hoursPage("2026-11-16", next: "own_c1"), "own_c1": hoursPage("2026-11-17", next: nil)]
    }

    /// "Show more" with a cursor the server refuses (400: an older shape, another walk): page one of
    /// the same week again, replacing the list, and no "can't reach" -- not a dead button.
    func testARefusedShowMoreReadsPageOneOfTheSameWeekQuietly() async {
        twoPages()
        await app.launch()
        let staff = app.staff
        await staff.loadHours(week: "2026-11-16", after: nil)
        XCTAssertEqual(staff.records.count, 2)
        await staff.loadHours(week: "2026-11-16", after: "own_old_unbound")
        XCTAssertEqual(server.hoursBodies.map { $0["after"] as? String }, [nil, "own_old_unbound", nil])
        XCTAssertEqual(server.hoursBodies.map { $0["week"] as? String }, ["2026-11-16", "2026-11-16", "2026-11-16"])
        XCTAssertFalse(staff.hoursFailed)
        XCTAssertEqual(staff.records.map(\.date), ["2026-11-16", "2026-11-16"], "replaced, not appended")
        XCTAssertEqual(staff.hours?.nextCursor, "own_c1", "Show more works again")
        XCTAssertEqual(activation.screen, .activated)
    }

    func testAGoodShowMoreAppendsTheNextPage() async {
        twoPages()
        await app.launch()
        let staff = app.staff
        await staff.loadHours(week: "2026-11-16", after: nil)
        await staff.loadHours(week: "2026-11-16", after: "own_c1")
        XCTAssertEqual(server.hoursBodies.count, 2)
        XCTAssertEqual(staff.records.map(\.date), ["2026-11-16", "2026-11-16", "2026-11-17", "2026-11-17"])
        XCTAssertNil(staff.hours?.nextCursor)
    }

    /// Page one refused is no cursor's fault: it is not read again, and it fails as before.
    func testARefusedFirstPageIsNotReadAgain() async {
        twoPages()
        await app.launch()
        await app.staff.loadHours(week: "2026-11-09", after: nil)
        XCTAssertEqual(server.hoursBodies.count, 1)
        XCTAssertTrue(app.staff.hoursFailed)
    }

    /// The browser's WholeWeek: "No records this day" only when no page is left to load.
    func testNoRecordsIsSaidOnlyWhenTheLoadedPagesHoldTheWholeWeek() async {
        twoPages()
        await app.launch()
        let staff = app.staff
        await staff.loadHours(week: "2026-11-16", after: nil)
        XCTAssertFalse(staff.saysNoRecords(on: "2026-11-17"), "Tuesday's records are on page two")
        XCTAssertFalse(staff.saysNoRecords(on: "2026-11-18"), "a later page may hold Wednesday's")
        await staff.loadHours(week: "2026-11-16", after: "own_c1")
        XCTAssertTrue(staff.saysNoRecords(on: "2026-11-18"))
        XCTAssertFalse(staff.saysNoRecords(on: "2026-11-17"))
        XCTAssertFalse(staff.saysNoRecords(on: "2026-11-16"))
    }

    // MARK: The missing-time sheet's hint

    /// Before a time is sent the hint names the window alone; "does not fit" is the server's
    /// answer to a sent time and stands only while the wheel still shows that time.
    func testTheClaimHintNamesTheWindowUntilASentTimeIsRefused() async {
        await app.launch()
        let staff = app.staff
        let window = StaffCopy.window(from: openClaim.windowFrom, to: openClaim.windowTo)
        let before = staff.claimHint(openClaim, picked: "18:00")
        XCTAssertEqual(before.text, StaffCopy.windowHint(window))
        XCTAssertFalse(before.problem)
        XCTAssertFalse(before.text.contains("does not fit"))

        server.claimResult = "outside_window"
        await staff.send(openClaim, at: "17:00")
        let refused = staff.claimHint(openClaim, picked: "17:00")
        XCTAssertEqual(refused.text, StaffCopy.outside(window))
        XCTAssertTrue(refused.problem)
        let moved = staff.claimHint(openClaim, picked: "19:30")
        XCTAssertEqual(moved.text, StaffCopy.windowHint(window), "a new pick has not been refused")
        XCTAssertFalse(moved.problem)
    }

    // MARK: Settings (M16-38)

    /// [G38-b]: after "Sign this phone out" the Keychain record -- the Secure Enclave key's only
    /// handle -- is gone, no device is held, and the code screen shows.
    func testSigningThisPhoneOutLeavesNoKeyHandle() async {
        await app.launch()
        XCTAssertNotNil(store.record)
        await app.staff.perform(.signOut)
        XCTAssertEqual(server.forgets, [AppClient.Path.revoke])
        XCTAssertNil(store.record, "the key's handle is cleared")
        XCTAssertNil(activation.device)
        XCTAssertNil(tap.identity)
        XCTAssertEqual(activation.screen, .code(notice: nil))
    }

    /// [G38-b]: the same after "Delete my account".
    func testTheDeletionRequestLeavesNoKeyHandle() async {
        await app.launch()
        await app.staff.perform(.delete)
        XCTAssertEqual(server.forgets, [AppClient.Path.accountDelete])
        XCTAssertNil(store.record, "the key's handle is cleared")
        XCTAssertNil(activation.device)
        XCTAssertNil(tap.identity)
        XCTAssertEqual(activation.screen, .code(notice: nil))
    }

    /// No answer forgets nothing: the record stays for another try and the dialog says it failed.
    func testAnUnansweredDeletionRequestKeepsTheKeyHandle() async {
        await app.launch()
        offline(paths: [AppClient.Path.nonce])
        await app.staff.perform(.delete)
        XCTAssertNotNil(store.record)
        XCTAssertNotNil(activation.device)
        XCTAssertTrue(app.staff.confirmFailed)
        XCTAssertEqual(app.staff.confirm, .delete)
    }

    /// A deactivated person's request signs nothing out (M16-37): once the server said
    /// "not_active", the deletion words lead with "Your request goes to <Business>." instead.
    func testNotActiveTurnsTheDeletionWordsToTheDeactivatedOnes() async {
        await app.launch()
        XCTAssertFalse(app.staff.notActive)
        server.claimResult = "not_active"
        await app.staff.send(openClaim, at: "22:00")
        XCTAssertTrue(app.staff.notActive)
        XCTAssertEqual(app.staff.claimHint(openClaim, picked: "22:00").text, StaffCopy.notActive)
        let active = StaffCopy.deleteBody("Kebab Factory")
        let deactivated = StaffCopy.deleteBody("Kebab Factory", deactivated: true)
        XCTAssertNotEqual(active, deactivated)
        XCTAssertTrue(deactivated.contains("Kebab Factory"))
        if !StaffCopy.turkish {
            XCTAssertTrue(active.hasPrefix("This signs you out on every phone"))
            XCTAssertTrue(deactivated.hasPrefix("Your request goes to Kebab Factory. Your clock-in records are not deleted"))
        }
    }

    /// App Store 5.1.1(i): the privacy notice is the app's own server's /legal/privacy.
    func testThePrivacyLinkIsTheServersNotice() {
        XCTAssertEqual(app.staff.privacyURL.absoluteString, "https://taptime.mt/legal/privacy")
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

    /// A 401 on a plaque's tap/start: the problem screen, and nothing says "at work" any more;
    /// "Start again" clears again on its way to activation (M16-46 trigger 3, M16-47).
    func testASignedOutPlaqueClearsTheLockScreen() async {
        server.meBody["presence"] = inPresence
        await app.launch()
        XCTAssertEqual(counter.live.count, 1)
        XCTAssertTrue(share.read().isIn)
        server.devicesSignedOut.insert(deviceID)
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: Copy.Problems.signedOut, retry: false,
                                                             browserURL: plaque, startAgain: true)))
        XCTAssertEqual(reminder.cancels, 1)
        XCTAssertTrue(counter.live.isEmpty)
        XCTAssertEqual(share.read(), .none)
        tap.startAgain()
        XCTAssertEqual(activation.screen, .code(notice: nil))
        XCTAssertEqual(reminder.cancels, 2)
        XCTAssertEqual(share.read(), .none)
    }

    /// A 401 on the press itself (tap/start passed): the same.
    func testASignedOutPressClearsTheLockScreen() async {
        server.meBody["presence"] = inPresence
        await app.launch()
        XCTAssertEqual(counter.live.count, 1)
        await app.open(plaque)
        server.refuseNonced = 2
        await tap.press(want: "out")
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: Copy.Problems.signedOut, retry: false,
                                                             browserURL: plaque, startAgain: true)))
        XCTAssertEqual(reminder.cancels, 1)
        XCTAssertTrue(counter.live.isEmpty)
        XCTAssertEqual(share.read(), .none)
        XCTAssertEqual(mePaths, 1, "nothing was recorded, so no /me follows")
    }

    /// A 401 at home: the activation screen, and the reminder goes with the share and the counter.
    func testASignedOutHomeClearsTheReminderToo() async {
        server.meBody["presence"] = inPresence
        await app.launch()
        XCTAssertEqual(counter.live.count, 1)
        server.devicesSignedOut.insert(deviceID)
        await tap.home()
        XCTAssertEqual(activation.screen, .code(notice: Copy.Problems.signedOut))
        XCTAssertEqual(reminder.cancels, 1)
        XCTAssertTrue(counter.live.isEmpty)
        XCTAssertEqual(share.read(), .none)
    }

    /// A clock days off is not "signed out": the last answer stands.
    func testAClockDaysOffClearsNothing() async {
        server.meBody["presence"] = inPresence
        await app.launch()
        server.now = local.addingTimeInterval(3 * 86_400)
        await app.open(plaque)
        XCTAssertEqual(tap.screen, .problem(TapModel.Problem(copy: TapCopy.Problems.clock)))
        XCTAssertEqual(reminder.cancels, 0)
        XCTAssertEqual(counter.live.count, 1)
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

    /// The plaque's printed static QR is sun.PlaqueQRURL's https://<host>/t?tag=<UID>: the plaque
    /// route itself, so the same gate lets it through as a plaque.
    func testThePrintedStaticQRIsThePlaqueRoute() {
        let printed = URL(string: "https://taptime.mt/t?tag=04A1B2C3D4E5F6")!
        XCTAssertEqual(TapLink.of(printed, host: host), .plaque(sun: "tag=04A1B2C3D4E5F6"))
    }
}

/// The live QR URL's gate (M16-22): the screens' codes are https://<host>/q?t=<token> and nothing
/// else; the plaque's rule applies, and a plaque URL is never a live QR (nor the other way round).
final class LiveQRURLTests: XCTestCase {
    let host = "taptime.mt"
    let q = "t=AQ00112233445566778899-_abcXYZ"

    func testOnlyTheScreensShapePasses() {
        XCTAssertEqual(LiveQRURL.query(of: URL(string: "https://taptime.mt/q?" + q)!, host: host), q)
        XCTAssertEqual(LiveQRURL.query(of: URL(string: "https://TAPTIME.MT/q?" + q)!, host: host), q)
        for bad in ["https://taptime.mt:8443/q?", "http://taptime.mt/q?", "https://taptime.mt/q/?", "https://evil.example/q?",
                    "https://taptime.mt.evil.example/q?", "https://taptime.mt/qr?", "https://taptime.mt/Q?", "https://taptime.mt/t?",
                    "https://taptime.mt/screen?"] {
            XCTAssertNil(LiveQRURL.query(of: URL(string: bad + q)!, host: host), bad)
        }
        for (user, password) in [("u", nil), ("u", "p")] as [(String, String?)] {
            var c = URLComponents(string: "https://taptime.mt/q?" + q)!
            c.user = user
            c.password = password
            XCTAssertNil(LiveQRURL.query(of: c.url!, host: host), user)
        }
        XCTAssertNil(LiveQRURL.query(of: URL(string: "https://taptime.mt/q?" + q + "#x")!, host: host))
        XCTAssertNil(LiveQRURL.query(of: URL(string: "https://taptime.mt/q")!, host: host))
    }

    func testATapLinkIsThePlaqueOrTheLiveQR() {
        XCTAssertEqual(TapLink.of(URL(string: "https://taptime.mt/q?" + q)!, host: host), .liveQR(query: q))
        XCTAssertEqual(TapLink.of(URL(string: "https://taptime.mt/t?tag=04A1B2C3D4E5F6")!, host: host), .plaque(sun: "tag=04A1B2C3D4E5F6"))
        XCTAssertNil(TapLink.of(URL(string: "https://taptime.mt/activate?code=x")!, host: host))
        XCTAssertEqual(TapLink.liveQR(query: q).browserURL(base: URL(string: "https://taptime.mt")!)?.absoluteString,
                       "https://taptime.mt/q?" + q)
    }

    /// The path is the server's: GET /q's literal and the screens' TapURL constant (read from the Go
    /// source, which the test bundle can reach from this checkout).
    func testThePathIsTheServers() throws {
        let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let token = try String(contentsOf: root.appendingPathComponent("internal/liveqr/token.go"), encoding: .utf8)
        let livetap = try String(contentsOf: root.appendingPathComponent("internal/handler/livetap.go"), encoding: .utf8)
        XCTAssertTrue(token.contains("LiveQRTapPath = \"\(LiveQRURL.path)\""), "liveqr.LiveQRTapPath")
        XCTAssertTrue(token.contains("TokenParam    = \"t\""), "liveqr.TokenParam")
        XCTAssertTrue(livetap.contains("const LiveQRPath = \"\(LiveQRURL.path)\""), "handler.LiveQRPath")
    }
}
