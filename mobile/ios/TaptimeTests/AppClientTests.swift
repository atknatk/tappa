import CryptoKit
import XCTest
@testable import Taptime

final class ServerClockTests: XCTestCase {
    func testParsesGosDateHeader() {
        XCTAssertEqual(ServerClock.parseHTTPDate("Sun, 06 Nov 1994 08:49:37 GMT")?.timeIntervalSince1970, 784_111_777)
        XCTAssertNil(ServerClock.parseHTTPDate("Sunday, 06-Nov-94 08:49:37 GMT"))
        XCTAssertNil(ServerClock.parseHTTPDate(""))
    }

    func testTheOffsetIsMeasuredAgainstTheRoundTripsMidpoint() {
        let local = Date(timeIntervalSince1970: 1_794_700_800)
        let c = ServerClock(localNow: { local })
        XCTAssertFalse(c.hasMeasurement)
        // Sent at local-4, received at local: the midpoint is local-2, the server said local+300.
        XCTAssertTrue(c.observe(dateHeader: httpDate(local.addingTimeInterval(300)),
                                sentAt: local.addingTimeInterval(-4), receivedAt: local))
        XCTAssertEqual(c.currentOffset, 302, accuracy: 0.001)
        XCTAssertEqual(c.now().timeIntervalSince1970, local.timeIntervalSince1970 + 302, accuracy: 0.001)
    }

    /// More than a day off is not the server's clock: the measurement is refused and the
    /// previous one stays; exactly a day is still taken (M16-16 review).
    func testAnOffsetBeyondADayIsIgnored() {
        let local = Date(timeIntervalSince1970: 1_794_700_800)
        let c = ServerClock(localNow: { local })
        XCTAssertTrue(c.observe(dateHeader: httpDate(local.addingTimeInterval(120)), sentAt: local, receivedAt: local))
        XCTAssertFalse(c.observe(dateHeader: httpDate(local.addingTimeInterval(86_401)), sentAt: local, receivedAt: local))
        XCTAssertFalse(c.observe(dateHeader: httpDate(local.addingTimeInterval(-86_401)), sentAt: local, receivedAt: local))
        XCTAssertFalse(c.observe(dateHeader: "Mon, 01 Jan 2001 00:00:00 GMT", sentAt: local, receivedAt: local))
        XCTAssertEqual(c.currentOffset, 120, accuracy: 0.001)
        XCTAssertTrue(c.observe(dateHeader: httpDate(local.addingTimeInterval(-86_400)), sentAt: local, receivedAt: local))
        XCTAssertEqual(c.currentOffset, -86_400, accuracy: 0.001)
    }

    func testAnUnreadableHeaderKeepsThePreviousMeasurement() {
        let local = Date(timeIntervalSince1970: 1_794_700_800)
        let c = ServerClock(localNow: { local })
        c.observe(dateHeader: httpDate(local.addingTimeInterval(-90)), sentAt: local, receivedAt: local)
        XCTAssertFalse(c.observe(dateHeader: nil, sentAt: local, receivedAt: local))
        XCTAssertFalse(c.observe(dateHeader: "garbage", sentAt: local, receivedAt: local))
        XCTAssertEqual(c.currentOffset, -90, accuracy: 0.001)
    }
}

/// The client against the surface's twin (Support.swift FakeAppServer), through URLSession and a
/// URLProtocol: the bytes these tests check are the bytes that would leave the phone.
final class AppClientTests: XCTestCase {
    /// The phone's clock: ten minutes BEHIND the server -- far outside the ±60 s read window.
    let local = Date(timeIntervalSince1970: 1_794_700_800)
    var server: FakeAppServer!
    var client: AppClient!

    override func setUp() {
        server = FakeAppServer(now: local.addingTimeInterval(600))
        let s = server!
        MockURLProtocol.handler = { req, body in try s.handle(req, body) }
        let l = local
        client = AppClient(baseURL: URL(string: "https://taptime.mt")!,
                           session: AppClient.makeSession(protocolClasses: [MockURLProtocol.self]),
                           clock: ServerClock(localNow: { l }))
    }

    override func tearDown() {
        MockURLProtocol.handler = nil
    }

    private func json(_ d: Data) -> [String: Any] {
        (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:]
    }

    func testLookupSendsOnlyTheCodeAndReadsTheAnswer() async throws {
        let r = try await client.lookup(code: FakeAppServer.code)
        XCTAssertEqual(r.employeeName, "Maria Borg")
        XCTAssertEqual(r.businessName, "Kebab Factory")
        XCTAssertEqual(r.wifiSsid, "KF-Staff")
        XCTAssertEqual(r.brand.accent, "1F5C41")
        let seen = try XCTUnwrap(server.seen.last)
        XCTAssertEqual(seen.method, "POST")
        XCTAssertEqual(seen.path, "/app/v1/activate/lookup")
        XCTAssertEqual(Array(json(seen.body).keys), ["code"])
        XCTAssertNil(seen.headers["App-Signature"])
        XCTAssertNil(seen.headers["Cookie"])
    }

    func testBindSendsCodeConsentAndTheRawPoint() async throws {
        let k = SoftwareDeviceKey()
        let r = try await client.bind(code: FakeAppServer.code, publicPoint: k.publicPoint)
        XCTAssertTrue(CanonicalText.validDeviceID(r.activationId))
        XCTAssertTrue(CanonicalText.validNonce(r.challenge))
        let body = json(try XCTUnwrap(server.seen.last).body)
        XCTAssertEqual(body.keys.sorted(), ["code", "consent", "publicKey"])
        XCTAssertEqual(body["consent"] as? Bool, true)
        XCTAssertEqual(Base64URL.decode(body["publicKey"] as? String ?? ""), k.publicPoint)
    }

    /// complete is signed by the bound key over the server's challenge, App-Device-Id being the
    /// activation id: the twin verifies it exactly as appdevice.VerifyActivation would.
    func testCompleteIsSignedByTheBoundKeyOverTheChallenge() async throws {
        let k = SoftwareDeviceKey()
        let b = try await client.bind(code: FakeAppServer.code, publicPoint: k.publicPoint)
        let sun = "tag=04A1B2C3D4E5F6&ctr=000011&cmac=0011223344556677"
        let done = try await client.complete(code: FakeAppServer.code, signer: k, binding: b, sun: sun)
        XCTAssertTrue(CanonicalText.validDeviceID(done.deviceId))
        let seen = try XCTUnwrap(server.seen.last)
        XCTAssertEqual(seen.headers["App-Device-Id"], b.activationId)
        XCTAssertEqual(seen.headers["App-Nonce"], b.challenge)
        XCTAssertNil(seen.headers["App-Timestamp"])
        let body = json(seen.body)
        XCTAssertEqual(body.keys.sorted(), ["code", "platform", "publicKey", "sun"])
        XCTAssertEqual(body["platform"] as? String, "ios")
        XCTAssertEqual(body["sun"] as? String, sun)
        XCTAssertEqual(server.completedSUN, [sun])
    }

    func testAnotherKeysSignatureOnCompleteIsRefused() async throws {
        let bound = SoftwareDeviceKey()
        let b = try await client.bind(code: FakeAppServer.code, publicPoint: bound.publicPoint)
        do {
            _ = try await client.complete(code: FakeAppServer.code, signer: SoftwareDeviceKey(), binding: b, sun: "tag=04&ctr=000001&cmac=00")
            XCTFail("an unbound key completed")
        } catch {
            XCTAssertEqual(error as? AppAPIError, .refused(.activationNotReady, retryAfter: nil))
        }
    }

    /// Clause (1) of the skew contract: the timestamp is the server's second, learnt from the Date
    /// header of an earlier answer -- the phone's own clock is ten minutes off and the read passes
    /// on the FIRST attempt.
    func testASignedReadUsesTheServerCorrectedClock() async throws {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        _ = try await client.lookup(code: FakeAppServer.code) // any answer measures the clock
        let before = server.seen.count
        let me = try await client.me(DeviceIdentity(deviceID: id, signer: k))
        XCTAssertEqual(me.employeeName, "Maria Borg")
        XCTAssertEqual(server.seen.count - before, 1)
        let seen = try XCTUnwrap(server.seen.last)
        XCTAssertEqual(seen.method, "GET")
        XCTAssertEqual(seen.path, "/app/v1/me")
        XCTAssertNil(seen.headers["App-Nonce"])
        XCTAssertEqual(seen.headers["App-Timestamp"], CanonicalText.timestamp(server.now))
    }

    /// Clause (2): the first read from a phone whose clock is ten minutes off is refused (401);
    /// that 401's Date header re-measures the clock and ONE retry passes.
    func testA401IsRetriedOnceWithAFreshMeasurement() async throws {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        let me = try await client.me(DeviceIdentity(deviceID: id, signer: k))
        XCTAssertEqual(me.direction, "in")
        XCTAssertEqual(server.seen.count, 2)
        XCTAssertEqual(server.seen[0].headers["App-Timestamp"], CanonicalText.timestamp(local))
        XCTAssertEqual(server.seen[1].headers["App-Timestamp"], CanonicalText.timestamp(server.now))
    }

    /// A second 401 is the signed-out answer -- and there is no third attempt.
    func testTwo401sAreSignedOut() async throws {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        server.devicesSignedOut.insert(id)
        do {
            _ = try await client.me(DeviceIdentity(deviceID: id, signer: k))
            XCTFail("a signed-out device read")
        } catch {
            XCTAssertEqual(error as? AppAPIError, .unauthenticated)
        }
        XCTAssertEqual(server.seen.count, 2)
    }

    func testANoncedRequestFetchesANonceAndSignsOverIt() async throws {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        try await client.revoke(DeviceIdentity(deviceID: id, signer: k))
        let paths = server.seen.map(\.path)
        XCTAssertEqual(paths.suffix(2), ["/app/v1/nonce", "/app/v1/revoke"])
        let revoke = try XCTUnwrap(server.seen.last)
        XCTAssertNotNil(revoke.headers["App-Nonce"])
        XCTAssertNil(revoke.headers["App-Timestamp"])
        XCTAssertEqual(server.seen[server.seen.count - 2].method, "POST")
    }

    /// A refused nonce'd request is retried once, over a NEW nonce.
    func testANonced401IsRetriedOnceWithANewNonce() async throws {
        let k = SoftwareDeviceKey()
        let id = server.register(k)
        _ = try await client.lookup(code: FakeAppServer.code)
        server.refuseNonced = 1
        try await client.revoke(DeviceIdentity(deviceID: id, signer: k))
        let revokes = server.seen.filter { $0.path == "/app/v1/revoke" }
        XCTAssertEqual(revokes.count, 2)
        XCTAssertNotEqual(revokes[0].headers["App-Nonce"], revokes[1].headers["App-Nonce"])
    }

    func testTheErrorVocabularyIsReadAndUnknownCodesAreUnexpected() async throws {
        let l = local
        for code in AppErrorCode.allCases {
            MockURLProtocol.handler = { _, _ in
                MockURLProtocol.Reply(status: code == .rateLimited ? 429 : 400,
                                      body: Data("{\"error\":\"\(code.rawValue)\"}\n".utf8),
                                      headers: ["Date": httpDate(l), "Retry-After": "7"])
            }
            do {
                _ = try await client.lookup(code: FakeAppServer.code)
                XCTFail(code.rawValue)
            } catch {
                XCTAssertEqual(error as? AppAPIError, .refused(code, retryAfter: 7), code.rawValue)
            }
        }
        MockURLProtocol.handler = { _, _ in
            MockURLProtocol.Reply(status: 400, body: Data("{\"error\":\"something_new\"}".utf8), headers: [:])
        }
        do {
            _ = try await client.lookup(code: FakeAppServer.code)
        } catch {
            XCTAssertEqual(error as? AppAPIError, .unexpected(status: 400))
        }
        MockURLProtocol.handler = { _, _ in throw URLError(.notConnectedToInternet) }
        do {
            _ = try await client.lookup(code: FakeAppServer.code)
        } catch {
            XCTAssertEqual(error as? AppAPIError, .transport)
        }
    }

    /// A 3xx is neither followed nor read: one request leaves the phone, the other host sees
    /// nothing, and the answer is a problem (M16-16 review).
    func testARedirectIsRefusedNotFollowed() async throws {
        var seen: [URL] = []
        let l = local
        MockURLProtocol.handler = { req, _ in
            seen.append(req.url!)
            if req.url?.host == "taptime.mt" {
                return MockURLProtocol.Reply(status: 302, body: nil,
                                             headers: ["Location": "https://evil.example/app/v1/activate/lookup",
                                                       "Date": httpDate(l.addingTimeInterval(-90_000))])
            }
            return MockURLProtocol.Reply(status: 200, body: Data(#"{"employeeName":"A","businessName":"B","secondDevice":false,"brand":{}}"#.utf8),
                                         headers: ["Date": httpDate(l)])
        }
        do {
            _ = try await client.lookup(code: FakeAppServer.code)
            XCTFail("a redirected lookup succeeded")
        } catch {
            XCTAssertEqual(error as? AppAPIError, .unexpected(status: 302))
        }
        XCTAssertEqual(seen.map { $0.host }, ["taptime.mt"])
        XCTAssertFalse(client.clock.hasMeasurement, "a redirect's Date header is not the server's")
    }

    /// The same for a signed read: the signature is not re-sent anywhere, and no retry follows.
    func testASignedReadIsNotRedirectedEither() async throws {
        var seen = 0
        let l = local
        MockURLProtocol.handler = { req, _ in
            seen += 1
            return MockURLProtocol.Reply(status: 307, body: nil,
                                         headers: ["Location": "https://evil.example/app/v1/me", "Date": httpDate(l)])
        }
        let k = SoftwareDeviceKey()
        do {
            _ = try await client.me(DeviceIdentity(deviceID: "6f1c2a9e-3b4d-4e5f-8a7b-0c1d2e3f4a5b", signer: k))
            XCTFail("a redirected read succeeded")
        } catch {
            XCTAssertEqual(error as? AppAPIError, .unexpected(status: 307))
        }
        XCTAssertEqual(seen, 1)
    }

    /// Through the client: an answer whose Date is two days off does not move the signing clock.
    func testAFarOffDateHeaderDoesNotMoveTheSigningClock() async throws {
        let l = local
        MockURLProtocol.handler = { _, _ in
            MockURLProtocol.Reply(status: 200, body: Data(#"{"employeeName":"A","businessName":"B","secondDevice":false,"brand":{}}"#.utf8),
                                  headers: ["Date": httpDate(l.addingTimeInterval(2 * 86_400))])
        }
        _ = try await client.lookup(code: FakeAppServer.code)
        XCTAssertFalse(client.clock.hasMeasurement)
        XCTAssertEqual(client.clock.now(), local)
    }

    func testAnswersWithExtraFieldsStillDecode() async throws {
        let l = local
        MockURLProtocol.handler = { _, _ in
            MockURLProtocol.Reply(status: 200, body: Data(#"{"employeeName":"A","businessName":"B","secondDevice":true,"brand":{},"later":1}"#.utf8),
                                  headers: ["Date": httpDate(l)])
        }
        let r = try await client.lookup(code: FakeAppServer.code)
        XCTAssertEqual(r.brand, .taptime)
        XCTAssertTrue(r.secondDevice)
        XCTAssertNil(r.locationName)
    }

    func testInviteCodesComeFromTheLinkOrStandAlone() {
        let c = FakeAppServer.code
        let h = "taptime.mt"
        XCTAssertEqual(InviteCode.parse("https://taptime.mt/activate?code=\(c)", host: h), c)
        XCTAssertEqual(InviteCode.parse("  https://TAPTIME.mt/activate?code=\(c)\n", host: h), c)
        XCTAssertEqual(InviteCode.parse(c, host: h), c, "a bare code is still accepted")
        XCTAssertEqual(InviteCode.parse("  \(c) ", host: h), c)
        XCTAssertNil(InviteCode.parse("https://taptime.mt/activate", host: h))
        XCTAssertNil(InviteCode.parse("https://taptime.mt/activate?code=short", host: h))
        XCTAssertNil(InviteCode.parse("hello", host: h))
    }

    /// A link counts only if it is this server's /activate link (M16-16 review).
    func testAnInviteLinkOfAnotherHostOrPathIsRefused() {
        let c = FakeAppServer.code
        let h = "taptime.mt"
        for link in [
            "https://taptime.mt.evil.example/activate?code=\(c)",
            "https://evil.example/activate?code=\(c)",
            "https://evil.example/?u=https://taptime.mt/activate&code=\(c)",
            "http://taptime.mt/activate?code=\(c)",
            "https://taptime.mt/t?code=\(c)",
            "https://taptime.mt/activate/x?code=\(c)",
            "taptime://activate?code=\(c)",
        ] {
            XCTAssertNil(InviteCode.parse(link, host: h), link)
        }
    }

    func testOnlyThisServersPlaqueURLsYieldAQuery() {
        let host = "taptime.mt"
        XCTAssertEqual(PlaqueURL.sunQuery(of: URL(string: "https://taptime.mt/t?tag=04A1&ctr=000011&cmac=00AA")!, host: host),
                       "tag=04A1&ctr=000011&cmac=00AA")
        XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: "https://evil.example/t?tag=04A1&ctr=1&cmac=0")!, host: host))
        XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: "http://taptime.mt/t?tag=04A1&ctr=1&cmac=0")!, host: host))
        XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: "https://taptime.mt/activate?code=x")!, host: host))
        XCTAssertNil(PlaqueURL.sunQuery(of: URL(string: "https://taptime.mt/t")!, host: host))
    }
}
