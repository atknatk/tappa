import XCTest
@testable import Taptime

/// The activation flow end to end against the surface's twin, through URLProtocol: code → welcome
/// → bind (a new key) → ready → tap → complete → the record kept. A software key stands in for the
/// Secure Enclave, so the flow does not depend on whether the run has one; the twin verifies
/// every signature it is sent.
@MainActor
final class ActivationModelTests: XCTestCase {
    let local = Date(timeIntervalSince1970: 1_794_700_800)
    let plaque = URL(string: "https://taptime.mt/t?tag=04A1B2C3D4E5F6&ctr=000012&cmac=8899AABBCCDDEEFF")!
    var server: FakeAppServer!
    var keys: SoftwareKeyFactory!
    var store: MemoryActivationStore!
    var reader: FakePlaqueReader!
    var model: ActivationModel!
    var tap: TapModel!
    var app: AppModel!

    override func setUp() async throws {
        server = FakeAppServer(now: local.addingTimeInterval(600))
        let s = server!
        MockURLProtocol.handler = { req, body in try s.handle(req, body) }
        keys = SoftwareKeyFactory()
        store = MemoryActivationStore()
        reader = FakePlaqueReader()
        let l = local
        let client = AppClient(baseURL: URL(string: "https://taptime.mt")!,
                               session: AppClient.makeSession(protocolClasses: [MockURLProtocol.self]),
                               clock: ServerClock(localNow: { l }))
        model = ActivationModel(client: client, keys: keys, store: store, reader: reader)
        tap = TapModel(client: client, reader: reader, location: FakeLocation(), browser: FakeBrowser(), now: { l })
        app = AppModel(activation: model, tap: tap, browser: FakeBrowser())
    }

    /// The tap screen's greeting, or nil when the tap screen is not showing.
    private var greeted: String? {
        guard model.screen == .activated, case .tap(let v, _) = tap.screen else {
            return nil
        }
        return v.employeeName
    }

    override func tearDown() async throws {
        MockURLProtocol.handler = nil
    }

    private func walkToTheTapStep() async {
        await model.start()
        XCTAssertEqual(model.screen, .code(notice: nil))
        model.codeInput = "https://taptime.mt/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        XCTAssertEqual(model.screen, .welcome)
        await model.agree()
        XCTAssertEqual(model.screen, .ready)
        model.ready()
        XCTAssertEqual(model.screen, .tap(problem: nil))
    }

    func testTheWholeFlowActivatesWithTheBoundKey() async throws {
        await walkToTheTapStep()
        XCTAssertEqual(reader.alerts, [Copy.Tap.sheet], "the tap step opens the scan sheet by itself")
        XCTAssertEqual(keys.made.count, 1)
        XCTAssertEqual(server.boundKeys, [keys.made[0].publicPoint])

        await model.plaqueURLOpened(plaque)

        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
        XCTAssertEqual(server.completedSUN, ["tag=04A1B2C3D4E5F6&ctr=000012&cmac=8899AABBCCDDEEFF"])
        let rec = try XCTUnwrap(store.record)
        XCTAssertTrue(CanonicalText.validDeviceID(rec.deviceId))
        XCTAssertEqual(rec.keyHandle, keys.made[0].persistentHandle)
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/activate/lookup", "/app/v1/activate/bind", "/app/v1/activate/complete"])
    }

    /// The read from the scan sheet takes the same road as a plaque link.
    func testTheScanSheetsReadCompletes() async throws {
        await walkToTheTapStep()
        reader.deliver(.url(plaque))
        for _ in 0..<200 where model.screen == .tap(problem: nil) {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
    }

    func testACancelledSheetStaysAndAFailedReadSaysSo() async {
        await walkToTheTapStep()
        reader.deliver(.cancelled)
        XCTAssertEqual(model.screen, .tap(problem: nil))
        XCTAssertFalse(model.scanning)
        model.scan()
        reader.deliver(.failed)
        XCTAssertEqual(model.screen, .tap(problem: Copy.Problems.tapFailed))
    }

    /// new_key_required (the key is registered already): nothing was consumed, so a NEW key is
    /// bound and the person taps once more.
    func testNewKeyRequiredBindsAFreshKey() async {
        await walkToTheTapStep()
        server.completeScript = [(409, "new_key_required")]
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .tap(problem: Copy.Problems.tapFailed))
        XCTAssertEqual(keys.made.count, 2)
        XCTAssertNotEqual(keys.made[0].publicPoint, keys.made[1].publicPoint)
        XCTAssertEqual(server.boundKeys.count, 2)
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
        XCTAssertEqual(store.record?.keyHandle, keys.made[1].persistentHandle)
    }

    func testTapRefusalsStayOnTheTapStep() async {
        await walkToTheTapStep()
        server.completeScript = [(400, "activation_tap_failed"), (400, "activation_needs_touch")]
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .tap(problem: Copy.Problems.tapFailed))
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .tap(problem: Copy.Problems.needsTouch))
        XCTAssertNil(store.record)
    }

    func testAnotherHostsURLIsNeverSent() async {
        await walkToTheTapStep()
        let before = server.seen.count
        await model.plaqueURLOpened(URL(string: "https://evil.example/t?tag=04&ctr=000001&cmac=00")!)
        XCTAssertEqual(model.screen, .tap(problem: Copy.Problems.tapFailed))
        XCTAssertEqual(server.seen.count, before)
    }

    /// M16-16 hand-off 2: a plaque link on a phone that is not set up is no longer swallowed -- it
    /// says so and offers Safari (the URL is unspent: nothing was sent).
    func testAPlaqueURLOnAPhoneNotSetUpSaysSoAndSendsNothing() async {
        await app.launch()
        await app.open(plaque)
        XCTAssertEqual(model.screen, .notSetUp(plaque))
        XCTAssertTrue(server.seen.isEmpty)
        model.retry(.toCode)
        XCTAssertEqual(model.screen, .code(notice: nil))
    }

    /// The not-set-up screen keeps only a plaque URL of this server, rebuilt from its checked
    /// query; anything else is not kept for Safari at all.
    func testTheNotSetUpScreenKeepsOnlyACheckedRebuiltPlaqueURL() async {
        await app.launch()
        await app.open(URL(string: "https://taptime.mt:444/t?tag=04&ctr=000001&cmac=00")!)
        XCTAssertEqual(model.screen, .code(notice: nil), "a port: not a plaque URL")
        await app.open(URL(string: "https://taptime.mt/t?tag=04&ctr=000001&cmac=00#frag")!)
        XCTAssertEqual(model.screen, .code(notice: nil), "a fragment: not a plaque URL")
        await app.open(URL(string: "https://Taptime.MT/t?tag=04A1B2C3D4E5F6&ctr=000012&cmac=8899AABBCCDDEEFF")!)
        XCTAssertEqual(model.screen, .notSetUp(plaque), "rebuilt from the server's origin")
    }

    /// A plaque link that arrives while the model is busy waits and is handled ONCE when the work
    /// is done (it used to be dropped). Here it arrives -- twice -- during "Agree and continue"'s
    /// bind, which the test holds open; when bind answers, the link completes the activation, and
    /// the second arrival did not run a second time (that would have left "This app isn't set up").
    func testALinkDuringBusyWaitsAndIsHandledOnce() async throws {
        let gate = DispatchSemaphore(value: 0)
        let s = server!
        MockURLProtocol.handler = { req, body in
            if req.url?.path == AppClient.Path.bind {
                gate.wait()
            }
            return try s.handle(req, body)
        }
        await app.launch()   // the waiting link is routed through AppModel.open, as in the app
        model.codeInput = "https://taptime.mt/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        let agreeing = Task { await model.agree() }
        for _ in 0..<300 where !model.busy {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertTrue(model.busy)
        await model.plaqueURLOpened(plaque)
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .welcome, "nothing happens while busy")
        gate.signal()
        await agreeing.value
        for _ in 0..<300 where model.screen != .done(name: "Maria Borg", secondDevice: false) {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
        try await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false), "handled once")
        XCTAssertEqual(server.completedSUN.count, 1)
    }

    /// A link that arrives while the record is still being read (.checking) waits for it.
    func testALinkDuringCheckingWaits() async throws {
        await model.start()
        model.retry(.relaunch)   // .checking, start() queued as a Task
        XCTAssertEqual(model.screen, .checking)
        await model.plaqueURLOpened(plaque)
        for _ in 0..<300 where model.screen == .checking || model.screen == .code(notice: nil) {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertEqual(model.screen, .notSetUp(plaque))
        XCTAssertTrue(server.seen.isEmpty)
    }

    /// "I'm ready" with the consent bound: the plaque completes the activation, as the browser's
    /// pending activation does on any /t tap.
    func testOnTheReadyStepAPlaqueCompletesTheActivation() async {
        await app.launch()
        model.codeInput = "https://taptime.mt/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        await model.agree()
        XCTAssertEqual(model.screen, .ready)
        await app.open(plaque)
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
    }

    /// Before the consent, the browser says "Finish the setup steps first"; so does the app.
    func testOnWelcomeAPlaqueSaysFinishTheStepsFirst() async {
        await app.launch()
        model.codeInput = "https://taptime.mt/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        XCTAssertEqual(model.screen, .welcome)
        await app.open(plaque)
        XCTAssertEqual(model.screen, .problem(Copy.Problems.notReady, retry: .toWelcome))
        XCTAssertEqual(server.seen.map(\.path), ["/app/v1/activate/lookup"])
    }

    /// After "Activation complete" the next plaque is an ordinary tap (§5): the tap screen of that
    /// read, nothing recorded until the press.
    func testAfterActivationTheNextPlaqueIsATap() async {
        await app.launch()
        await walkToTheTapStepFromHere()
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
        await app.open(plaque)
        XCTAssertEqual(model.screen, .activated)
        guard case .tap(let v, nil) = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
        XCTAssertEqual(v.locationName, "St Julians")
        XCTAssertTrue(server.taps.isEmpty)
    }

    /// "Activation complete" has no button; back in front, the app opens on its tap screen.
    func testAfterActivationTheAppComesBackOnItsTapScreen() async throws {
        await app.launch()
        await walkToTheTapStepFromHere()
        await model.plaqueURLOpened(plaque)
        app.scenePhase(.background)
        app.scenePhase(.active)
        for _ in 0..<200 where greeted == nil {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertEqual(greeted, "Maria Borg")
    }

    private func walkToTheTapStepFromHere() async {
        model.codeInput = "https://taptime.mt/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        await model.agree()
        model.ready()
    }

    func testAnotherHostsInviteLinkIsNotSent() async {
        await model.start()
        model.codeInput = "https://evil.example/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        XCTAssertEqual(model.codeError, Copy.Code.notALink)
        XCTAssertTrue(server.seen.isEmpty)
    }

    func testABadInputIsNotSent() async {
        await model.start()
        model.codeInput = "not a link"
        await model.submitCode()
        XCTAssertEqual(model.codeError, Copy.Code.notALink)
        XCTAssertTrue(server.seen.isEmpty)
    }

    func testAnUnusableCodeIsTheBadLinkScreen() async {
        await model.start()
        model.codeInput = "AQECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
        await model.submitCode()
        XCTAssertEqual(model.screen, .problem(Copy.Problems.badLink, retry: .toCode))
    }

    func testWithoutASecureChipOrAReaderNothingStarts() async {
        keys.isAvailable = false
        await model.start()
        XCTAssertEqual(model.screen, .problem(Copy.Problems.noSecureChip, retry: .none))
        keys.isAvailable = true
        reader.isAvailable = false
        await model.start()
        XCTAssertEqual(model.screen, .problem(Copy.Problems.noReader, retry: .none))
        XCTAssertTrue(server.seen.isEmpty)
    }

    // MARK: Launch of an activated phone

    func testAnActivatedPhoneIsGreetedByTheServersName() async {
        let k = SoftwareDeviceKey()
        store.record = ActivationRecord(deviceId: server.register(k), employeeName: "Old Name", keyHandle: k.persistentHandle)
        await app.launch()
        XCTAssertEqual(greeted, "Maria Borg")
        XCTAssertNotNil(store.record)
    }

    /// Two 401s (after the clock retry) are the signed-out answer: the code screen says so and
    /// offers activation again -- and the record STAYS (a 401 may be the server's fault, and the
    /// Secure Enclave key behind a deleted record could never come back).
    func testTwo401sKeepTheRecordAndOfferActivation() async {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        server.devicesSignedOut.insert(id)
        let rec = ActivationRecord(deviceId: id, employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        store.record = rec
        await app.launch()
        XCTAssertEqual(model.screen, .code(notice: Copy.Problems.signedOut))
        XCTAssertEqual(store.record, rec)
        XCTAssertEqual(server.seen.count, 2)
    }

    /// The server comes back: the kept record works again, nothing to re-activate.
    func testAKeptRecordWorksOnceTheServerAnswersAgain() async {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        server.devicesSignedOut.insert(id)
        store.record = ActivationRecord(deviceId: id, employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        await app.launch()
        XCTAssertEqual(model.screen, .code(notice: Copy.Problems.signedOut))
        server.devicesSignedOut.remove(id)
        await app.open(plaque)
        XCTAssertEqual(model.screen, .activated, "the kept device answers again")
        guard case .tap = tap.screen else {
            return XCTFail("\(tap.screen)")
        }
    }

    /// Only a NEW successful activation replaces the kept record.
    func testANewActivationReplacesTheKeptRecord() async throws {
        let old = SoftwareDeviceKey()
        let id = server.register(old)
        server.devicesSignedOut.insert(id)
        store.record = ActivationRecord(deviceId: id, employeeName: "Maria Borg", keyHandle: old.persistentHandle)
        await app.launch()
        XCTAssertEqual(model.screen, .code(notice: Copy.Problems.signedOut))
        model.codeInput = "https://taptime.mt/activate?code=\(FakeAppServer.code)"
        await model.submitCode()
        await model.agree()
        model.ready()
        XCTAssertEqual(store.record?.keyHandle, old.persistentHandle, "nothing is replaced before complete")
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .done(name: "Maria Borg", secondDevice: false))
        let rec = try XCTUnwrap(store.record)
        XCTAssertNotEqual(rec.deviceId, id)
        XCTAssertEqual(rec.keyHandle, keys.made.last?.persistentHandle)
    }

    /// One 401 is not enough: the retry passes and the phone stays activated.
    func testOne401IsNotSignedOut() async {
        let k = SoftwareDeviceKey()
        store.record = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        server.refuseReads = 1
        await app.launch()
        XCTAssertEqual(greeted, "Maria Borg")
        XCTAssertNotNil(store.record)
    }

    /// A restore that fails once is retried once and the phone carries on.
    func testOneFailedRestoreIsRetried() async {
        let k = SoftwareDeviceKey()
        store.record = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        keys.failRestores = 1
        await app.launch()
        XCTAssertEqual(keys.restores, 2)
        XCTAssertEqual(greeted, "Maria Borg")
    }

    /// Two failures: a "try again" screen, the record kept, nothing sent; the next try works.
    func testARestoreThatKeepsFailingKeepsTheRecordAndOffersTryAgain() async throws {
        let k = SoftwareDeviceKey()
        let rec = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        store.record = rec
        keys.failRestores = 2
        await app.launch()
        XCTAssertEqual(keys.restores, 2, "one retry, no more")
        XCTAssertEqual(model.screen, .problem(Copy.Problems.keyUnreadable, retry: .relaunch))
        XCTAssertEqual(store.record, rec)
        XCTAssertTrue(server.seen.isEmpty)

        model.retry(.relaunch)
        for _ in 0..<200 where greeted == nil {
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        XCTAssertEqual(greeted, "Maria Borg", "the relaunch lands on the tap screen")
        XCTAssertEqual(store.record, rec)
    }

    /// "Start again" from that screen activates anew without deleting first.
    func testStartAgainFromAnUnreadableKeyKeepsTheRecordUntilANewActivation() async {
        let k = SoftwareDeviceKey()
        let rec = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        store.record = rec
        keys.failRestores = 2
        await app.launch()
        model.retry(.toCode)
        XCTAssertEqual(model.screen, .code(notice: nil))
        XCTAssertEqual(store.record, rec)
    }

    func testOfflineIsNotSignedOut() async {
        let k = SoftwareDeviceKey()
        store.record = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        MockURLProtocol.handler = { _, _ in throw URLError(.notConnectedToInternet) }
        await app.launch()
        XCTAssertEqual(greeted, "Maria Borg")
        XCTAssertNotNil(store.record)
    }
}

final class BrandLookTests: XCTestCase {
    func testTaptimesLookWithoutAnAccent() {
        let l = BrandLook(.taptime)
        XCTAssertFalse(l.accented)
        XCTAssertFalse(l.buttonEdge)
    }

    /// The design table of ADR 0023 §3: dark accents carry paper text, light ones ink text and an
    /// ink edge.
    func testTheButtonTextAndEdgeFollowTheContrastRule() {
        let green = BrandLook(AppBrand(name: nil, logo: nil, accent: "1F5C41"))
        XCTAssertEqual(green.buttonText, Palette.paper)
        XCTAssertFalse(green.buttonEdge)
        let yellow = BrandLook(AppBrand(name: nil, logo: nil, accent: "FFC72C"))
        XCTAssertEqual(yellow.buttonText, Palette.ink)
        XCTAssertTrue(yellow.buttonEdge)
        XCTAssertEqual(BrandLook.contrast(BrandLook.RGB.hex("1F5C41")!, .paper), 7.73, accuracy: 0.01)
        XCTAssertEqual(BrandLook.contrast(BrandLook.RGB.hex("FFC72C")!, .ink), 10.56, accuracy: 0.01)
        XCTAssertFalse(BrandLook(AppBrand(name: nil, logo: nil, accent: "#1F5C41")).accented)
    }
}

/// CLAUDE.md §9: the accent fills the primary button of the wizard's steps 1 and 2 and nothing else.
final class PrimaryLookTests: XCTestCase {
    let brand = AppBrand(name: nil, logo: nil, accent: "DA291C")

    func testTheAccentFillsStepsOneAndTwo() {
        for screen: ActivationModel.Screen in [.welcome, .ready] {
            XCTAssertEqual(screen.primaryLook(brand), BrandLook(brand), "\(screen)")
            XCTAssertTrue(screen.primaryLook(brand).accented, "\(screen)")
        }
    }

    func testEveryOtherScreenIsTaptimesGreen() {
        let others: [ActivationModel.Screen] = [
            .checking, .code(notice: nil), .code(notice: Copy.Problems.signedOut), .tap(problem: nil),
            .tap(problem: Copy.Problems.tapFailed), .done(name: "A", secondDevice: false),
            .activated, .notSetUp(URL(string: "https://taptime.mt/t?tag=04")!), .problem(Copy.Problems.badLink, retry: .toCode),
        ]
        for screen in others {
            XCTAssertFalse(screen.primaryLook(brand).accented, "\(screen)")
        }
    }

    /// The step-3 scan button draws the rule's look (green), while the step's marks take the brand's.
    func testTheScanButtonTakesTheRulesLook() throws {
        let src = try String(contentsOf: URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().appendingPathComponent("Taptime/ActivationViews.swift"), encoding: .utf8)
        XCTAssertTrue(src.contains("PrimaryButton(title: Copy.Tap.scan, look: buttonLook)"))
        XCTAssertFalse(src.contains("BrandLook(.taptime)"), "the views take their button look from primaryLook")
    }
}
