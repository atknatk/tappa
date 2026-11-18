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

    func testAPlaqueURLOutsideTheTapStepIsIgnored() async {
        await model.start()
        await model.plaqueURLOpened(plaque)
        XCTAssertEqual(model.screen, .code(notice: nil))
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
        await model.start()
        XCTAssertEqual(model.screen, .already(name: "Maria Borg"))
        XCTAssertNotNil(store.record)
    }

    /// Two 401s (after the clock retry) mean the server signed this phone out: the key is
    /// forgotten and the code screen says why.
    func testASignedOutPhoneForgetsItsKey() async {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        server.devicesSignedOut.insert(id)
        store.record = ActivationRecord(deviceId: id, employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        await model.start()
        XCTAssertEqual(model.screen, .code(notice: Copy.Problems.signedOut))
        XCTAssertNil(store.record)
        XCTAssertEqual(server.seen.count, 2)
    }

    /// One 401 is not enough: the retry passes and the phone stays activated.
    func testOne401IsNotSignedOut() async {
        let k = SoftwareDeviceKey()
        store.record = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        server.refuseReads = 1
        await model.start()
        XCTAssertEqual(model.screen, .already(name: "Maria Borg"))
        XCTAssertNotNil(store.record)
    }

    func testOfflineIsNotSignedOut() async {
        let k = SoftwareDeviceKey()
        store.record = ActivationRecord(deviceId: server.register(k), employeeName: "Maria Borg", keyHandle: k.persistentHandle)
        MockURLProtocol.handler = { _, _ in throw URLError(.notConnectedToInternet) }
        await model.start()
        XCTAssertEqual(model.screen, .already(name: "Maria Borg"))
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
