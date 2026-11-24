import CryptoKit
import Foundation
@testable import Taptime

// TEST-ONLY PARTS. The flow tests sign with a SOFTWARE P-256 key so that they run the same on a
// simulator with or without a Secure Enclave (the iOS 26.3 one on the build Mac has one; older
// ones may not) and can read every key back -- and that key exists only in this target. The app target has no software key type
// and no path to one (SourceGuardTests holds that); on a device without a Secure Enclave the app
// refuses to activate (DeviceKeyPolicyTests, ActivationModelTests).

struct SoftwareDeviceKey: DeviceSigner {
    let key: P256.Signing.PrivateKey

    init(key: P256.Signing.PrivateKey = P256.Signing.PrivateKey()) {
        self.key = key
    }

    var publicPoint: Data { key.publicKey.x963Representation }
    var persistentHandle: Data { key.rawRepresentation }

    func sign(_ message: Data) throws -> Data {
        try key.signature(for: message).derRepresentation
    }
}

final class SoftwareKeyFactory: DeviceKeyFactory {
    var isAvailable = true
    /// The next n restores fail, as a Secure Enclave that cannot open a handle would.
    var failRestores = 0
    private(set) var restores = 0
    private(set) var made: [SoftwareDeviceKey] = []

    func newKey() throws -> DeviceSigner {
        let k = SoftwareDeviceKey()
        made.append(k)
        return k
    }

    func restore(_ handle: Data) throws -> DeviceSigner {
        restores += 1
        if failRestores > 0 {
            failRestores -= 1
            throw DeviceKeyError.keyUnavailable
        }
        return SoftwareDeviceKey(key: try P256.Signing.PrivateKey(rawRepresentation: handle))
    }
}

final class MemoryActivationStore: ActivationStore {
    var record: ActivationRecord?

    func load() -> ActivationRecord? { record }
    func save(_ record: ActivationRecord) throws { self.record = record }
    func clear() { record = nil }
}

/// The press's location reading: what it answers, and whether it answers at once or when the test
/// says so. Counts every reading started -- §4.2 says one per press and none otherwise.
final class FakeLocation: TapLocating {
    var answer = TapLocationReport(lat: 35.9189, lng: 14.4893, mock: false)
    /// Hold the answer until deliver() (a fix that takes its time).
    var hold = false
    private(set) var reads: [Date] = []
    private var pending: ((TapLocationReport) -> Void)?

    func readOnce(pressedAt: Date, timeout: TimeInterval, completion: @escaping (TapLocationReport) -> Void) {
        reads.append(pressedAt)
        if hold {
            pending = completion
        } else {
            completion(answer)
        }
    }

    func deliver() {
        let p = pending
        pending = nil
        p?(answer)
    }
}

final class FakeBrowser: BrowserOpening {
    private(set) var opened: [URL] = []
    func open(_ url: URL) { opened.append(url) }
}

final class FakePlaqueReader: PlaqueReading {
    var isAvailable = true
    private(set) var alerts: [String] = []
    var pending: ((PlaqueReadResult) -> Void)?

    func read(alert: String, completion: @escaping (PlaqueReadResult) -> Void) {
        alerts.append(alert)
        pending = completion
    }

    func deliver(_ r: PlaqueReadResult) {
        let p = pending
        pending = nil
        p?(r)
    }
}

/// The system's Live Activities, in memory: what is running and every set of operations asked for.
final class FakeCounter: CounterDriving {
    var allowed = true
    var live: [RunningCounter] = []
    private(set) var performed: [[CounterOp]] = []
    private var next = 0

    func running() -> [RunningCounter] { live }

    func perform(_ ops: [CounterOp]) {
        performed.append(ops)
        for op in ops {
            switch op {
            case let .start(s, staleAt):
                next += 1
                live.append(RunningCounter(id: "a\(next)", state: s, staleAt: staleAt))
            case let .update(id, s, staleAt):
                live = live.map { $0.id == id ? RunningCounter(id: id, state: s, staleAt: staleAt) : $0 }
            case let .end(id):
                live.removeAll { $0.id == id }
            }
        }
    }
}

/// The reminder, counted.
final class FakeReminder: ReminderScheduling {
    private(set) var applied: [MeResponse?] = []
    private(set) var cancels = 0

    func apply(_ me: MeResponse?, offset: TimeInterval) { applied.append(me) }
    func cancel() { cancels += 1 }
}

// MARK: - URLProtocol

/// Routes every request of a session made with AppClient.makeSession(protocolClasses:) to
/// `handler`, which sees the request and its body bytes exactly as URLSession sends them.
final class MockURLProtocol: URLProtocol {
    struct Reply {
        var status: Int
        var body: Data?
        var headers: [String: String] = [:]
    }

    static var handler: ((URLRequest, Data) throws -> Reply)?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let body = MockURLProtocol.bodyBytes(of: request)
        do {
            guard let h = MockURLProtocol.handler else {
                throw URLError(.badServerResponse)
            }
            let reply = try h(request, body)
            let resp = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1",
                                       headerFields: reply.headers)!
            // A 3xx with a Location is offered to the session as a redirect first, as the URL
            // loading system does for HTTP; if the session declines it, the 3xx is the answer.
            if (300...399).contains(reply.status), let loc = reply.headers["Location"], let to = URL(string: loc) {
                var next = request
                next.url = to
                client?.urlProtocol(self, wasRedirectedTo: next, redirectResponse: resp)
            }
            client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
            if let d = reply.body {
                client?.urlProtocol(self, didLoad: d)
            }
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}

    /// URLSession hands a protocol the body as a stream, not as httpBody.
    static func bodyBytes(of req: URLRequest) -> Data {
        if let b = req.httpBody {
            return b
        }
        guard let s = req.httpBodyStream else {
            return Data()
        }
        s.open()
        defer { s.close() }
        var out = Data()
        var buf = [UInt8](repeating: 0, count: 4096)
        while s.hasBytesAvailable {
            let n = s.read(&buf, maxLength: buf.count)
            if n <= 0 {
                break
            }
            out.append(buf, count: n)
        }
        return out
    }
}

func httpDate(_ d: Date) -> String {
    let f = DateFormatter()
    f.locale = Locale(identifier: "en_US_POSIX")
    f.timeZone = TimeZone(identifier: "GMT")
    f.dateFormat = "EEE, dd MMM yyyy HH:mm:ss 'GMT'"
    return f.string(from: d)
}

// MARK: - A twin of the /app/v1 surface

/// A small in-memory twin of internal/handler's app surface: it checks what the real one checks
/// at the boundary -- strict body fields, the canonical text, the signature under the right key,
/// the ±60 s read window against ITS clock, single-use nonces -- and answers with a Date header
/// on every status. It is how the tests prove the app's requests would verify, without a server.
final class FakeAppServer {
    struct Seen {
        let method: String
        let path: String
        let headers: [String: String]
        let body: Data
    }

    static let code = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

    /// The server's clock. The phone's clock is the test's `local`.
    var now: Date
    var lookupBody: [String: Any] = [
        "employeeName": "Maria Borg", "businessName": "Kebab Factory", "locationName": "St Julians",
        "wifiSsid": "KF-Staff", "secondDevice": false, "brand": ["accent": "1F5C41"],
    ]
    /// Scripted answers for complete, consumed in order before the normal path runs.
    var completeScript: [(Int, String?)] = []
    var meBody: [String: Any] = ["employeeName": "Maria Borg", "brand": [String: Any](), "direction": "in"]
    var tapStartBody: [String: Any] = [
        "employeeName": "Maria Borg", "locationName": "St Julians", "brand": ["accent": "DA291C"],
        "direction": "in", "qr": false, "context": "signed-context-1",
    ]
    var tapBody: [String: Any] = [
        "outcome": "recorded", "verdict": "ok", "direction": "in", "venue": "St Julians", "at": "09:12",
        "businessType": "restaurant",
    ]
    /// Scripted answers for tap/start and tap, consumed in order before the normal path runs.
    var tapStartScript: [(Int, String?)] = []
    var tapScript: [(Int, String?)] = []
    /// The sun queries tap/start was sent, and the bodies tap was sent (as JSON objects).
    private(set) var tapStarts: [String] = []
    /// The live QR queries tap/start was sent as `liveQR` (M16-22).
    private(set) var liveQRStarts: [String] = []
    private(set) var taps: [[String: Any]] = []
    /// Answer 401 to the next n signed reads, whatever they carry.
    var refuseReads = 0
    /// Answer 401 to the next n nonce'd requests.
    var refuseNonced = 0
    var devicesSignedOut = Set<String>()
    /// claims/send's result word ("sent", "outside_window", …).
    var claimResult = "sent"

    // M16-23: the phone that cannot tap.
    /// The live QR queries complete accepted as `liveQR`, and the one it refuses (one code, §4.7).
    private(set) var completedLiveQR: [String] = []
    var liveQRRefused: String?
    /// Every complete body's field names, in order.
    private(set) var completeFields: [[String]] = []
    /// Scripted answers for approval, consumed in order before the normal path ("waiting").
    var approvalScript: [(Int, String?)] = []
    /// approval/status answers, consumed in order: "waiting", "approved", "rejected", "expired", or
    /// "409:new_key_required"; empty is "waiting".
    var statusScript: [String] = []
    private(set) var approvals = 0
    private(set) var polls = 0
    /// The paths whose activation signature verified (each route signs its own path).
    private(set) var approvalPaths: [String] = []
    static let pairingCode = "482913"

    private(set) var seen: [Seen] = []
    private(set) var boundKeys: [Data] = []
    private(set) var completedSUN: [String] = []
    private var challenge: (id: String, value: String, key: Data)?
    private var devices: [String: Data] = [:]
    private var liveNonces = Set<String>()

    init(now: Date) {
        self.now = now
    }

    func handle(_ req: URLRequest, _ body: Data) throws -> MockURLProtocol.Reply {
        let method = req.httpMethod ?? "GET"
        let path = req.url?.path ?? ""
        let headers = req.allHTTPHeaderFields ?? [:]
        seen.append(Seen(method: method, path: path, headers: headers, body: body))
        if req.url?.query != nil {
            return reply(401)
        }
        switch path {
        case AppClient.Path.lookup:
            guard fields(body) == ["code"], (json(body)["code"] as? String) == FakeAppServer.code else {
                return reply(400, error: "invalid_invite")
            }
            return reply(200, object: lookupBody)
        case AppClient.Path.bind:
            let j = json(body)
            guard fields(body) == ["code", "consent", "publicKey"], (j["code"] as? String) == FakeAppServer.code else {
                return reply(400, error: "bad_request")
            }
            guard j["consent"] as? Bool == true else {
                return reply(400, error: "consent_required")
            }
            guard let pk = Base64URL.decode(j["publicKey"] as? String ?? ""), DevicePublicKey.isValidPoint(pk) else {
                return reply(400, error: "invalid_key")
            }
            let id = UUID().uuidString.lowercased()
            let value = Base64URL.encode(Data((0..<32).map { _ in UInt8.random(in: 0...255) }))
            challenge = (id, value, pk)
            boundKeys.append(pk)
            return reply(200, object: ["activationId": id, "challenge": value])
        case AppClient.Path.complete:
            let j = json(body)
            completeFields.append(fields(body))
            // EXACTLY ONE of `sun` and `liveQR` (M16-23); both, or neither, is 400.
            guard fields(body) == ["code", "platform", "publicKey", "sun"] || fields(body) == ["code", "liveQR", "platform", "publicKey"],
                  j["platform"] as? String == "ios",
                  let pk = Base64URL.decode(j["publicKey"] as? String ?? "") else {
                return reply(400, error: "bad_request")
            }
            // The server's order: the proof of possession is PURE (appdevice.VerifyActivation) --
            // the signature under the key the body names, over the id and challenge the headers
            // carry -- and a refusal is 401. Whether that binding is still the invitation's is
            // asked later (invite.ConsentBound), and a stale or foreign one is 409
            // activation_not_ready, not 401.
            guard let id = headers[AppClient.Header.deviceID], let n = headers[AppClient.Header.nonce],
                  headers[AppClient.Header.timestamp] == nil,
                  let text = try? CanonicalText.request(method: method, path: path, body: body, nonce: n, deviceID: id),
                  verify(pk, headers[AppClient.Header.signature], text) else {
                return reply(401)
            }
            if !completeScript.isEmpty {
                let (status, code) = completeScript.removeFirst()
                return reply(status, error: code)
            }
            guard let ch = challenge, ch.id == id, ch.value == n, ch.key == pk else {
                return reply(409, error: "activation_not_ready")
            }
            if let live = j["liveQR"] as? String {
                if live == liveQRRefused {
                    return reply(400, error: "activation_tap_failed")
                }
                completedLiveQR.append(live)
            } else {
                completedSUN.append(j["sun"] as? String ?? "")
            }
            let device = UUID().uuidString.lowercased()
            devices[device] = pk
            challenge = nil
            return reply(200, object: ["deviceId": device, "employeeName": "Maria Borg"])
        case AppClient.Path.approval, AppClient.Path.approvalStatus:
            let j = json(body)
            guard fields(body) == ["code", "platform", "publicKey"], j["platform"] as? String == "ios",
                  let pk = Base64URL.decode(j["publicKey"] as? String ?? "") else {
                return reply(400, error: "bad_request")
            }
            // Signed like complete, over THIS route's path.
            guard let id = headers[AppClient.Header.deviceID], let n = headers[AppClient.Header.nonce],
                  headers[AppClient.Header.timestamp] == nil,
                  let text = try? CanonicalText.request(method: method, path: path, body: body, nonce: n, deviceID: id),
                  verify(pk, headers[AppClient.Header.signature], text) else {
                return reply(401)
            }
            guard (j["code"] as? String) == FakeAppServer.code else {
                return reply(400, error: "invalid_invite")
            }
            guard let ch = challenge, ch.id == id, ch.value == n, ch.key == pk else {
                return reply(409, error: "activation_not_ready")
            }
            approvalPaths.append(path)
            let waiting: [String: Any] = ["state": "waiting", "pairingCode": FakeAppServer.pairingCode,
                                          "expiresAt": "2026-11-20T10:15:00Z"]
            if path == AppClient.Path.approval {
                if !approvalScript.isEmpty {
                    let (status, code) = approvalScript.removeFirst()
                    return reply(status, error: code)
                }
                approvals += 1
                return reply(200, object: waiting)
            }
            polls += 1
            let next = statusScript.isEmpty ? "waiting" : statusScript.removeFirst()
            switch next {
            case "waiting":
                return reply(200, object: waiting)
            case "approved":
                // The device is born in this call, exactly as a successful complete.
                let device = UUID().uuidString.lowercased()
                devices[device] = pk
                challenge = nil
                return reply(200, object: ["state": "approved", "deviceId": device, "employeeName": "Maria Borg"])
            case "rejected", "expired":
                return reply(200, object: ["state": next])
            default:
                let parts = next.split(separator: ":")
                return reply(Int(parts[0]) ?? 500, error: String(parts[1]))
            }
        case AppClient.Path.tapStart:
            guard readVerifies(method, path, body, headers) else {
                return reply(401)
            }
            // Exactly one of the two fields (apptap.go's tapStart / tapStartLive, M16-22).
            if method == "POST", fields(body) == ["liveQR"], let q = json(body)["liveQR"] as? String {
                liveQRStarts.append(q)
            } else if method == "POST", fields(body) == ["sun"], let sun = json(body)["sun"] as? String {
                tapStarts.append(sun)
            } else {
                return reply(400, error: "bad_request")
            }
            if !tapStartScript.isEmpty {
                let (status, code) = tapStartScript.removeFirst()
                return reply(status, error: code)
            }
            return reply(200, object: tapStartBody)
        case AppClient.Path.tap:
            guard noncedVerifies(method, path, body, headers) else {
                return reply(401)
            }
            // The real surface decodes strictly: a field outside the contract is a 400.
            let allowed: Set<String> = ["context", "want", "lat", "lng", "mockLocation", "occurredAt"]
            guard method == "POST", Set(fields(body)).isSubset(of: allowed), json(body)["context"] is String else {
                return reply(400, error: "bad_request")
            }
            taps.append(json(body))
            if !tapScript.isEmpty {
                let (status, code) = tapScript.removeFirst()
                return reply(status, error: code)
            }
            return reply(200, object: tapBody)
        case AppClient.Path.me, AppClient.Path.nonce:
            guard readVerifies(method, path, body, headers) else {
                return reply(401)
            }
            if path == AppClient.Path.me {
                return reply(200, object: meBody)
            }
            let n = Base64URL.encode(Data((0..<32).map { _ in UInt8.random(in: 0...255) }))
            liveNonces.insert(n)
            return reply(200, object: ["nonce": n, "expiresAt": "2026-11-18T10:00:00Z"])
        // The staff screens' reads and the one claim write (M16-43, M16-31): only their signature
        // is checked here -- the tests drive the 401 path: verified, My hours is off and nothing is missing.
        case AppClient.Path.hours, AppClient.Path.claims:
            guard readVerifies(method, path, body, headers) else {
                return reply(401)
            }
            return path == AppClient.Path.claims ? reply(200, object: ["open": [Any]()]) : reply(404, error: "not_found")
        case AppClient.Path.claimsSend:
            guard noncedVerifies(method, path, body, headers) else {
                return reply(401)
            }
            return reply(200, object: ["result": claimResult])
        case AppClient.Path.revoke:
            guard noncedVerifies(method, path, body, headers) else {
                return reply(401)
            }
            return reply(204)
        default:
            return reply(404, error: "not_found")
        }
    }

    private func readVerifies(_ method: String, _ path: String, _ body: Data, _ h: [String: String]) -> Bool {
        if refuseReads > 0 {
            refuseReads -= 1
            return false
        }
        guard h[AppClient.Header.nonce] == nil, let id = h[AppClient.Header.deviceID], let key = devices[id],
              !devicesSignedOut.contains(id), let ts = h[AppClient.Header.timestamp], let sec = Int64(ts),
              abs(Double(sec) - now.timeIntervalSince1970) <= 60,
              let text = try? CanonicalText.read(method: method, path: path, body: body, timestamp: ts, deviceID: id) else {
            return false
        }
        return verify(key, h[AppClient.Header.signature], text)
    }

    private func noncedVerifies(_ method: String, _ path: String, _ body: Data, _ h: [String: String]) -> Bool {
        if refuseNonced > 0 {
            refuseNonced -= 1
            return false
        }
        guard h[AppClient.Header.timestamp] == nil, let id = h[AppClient.Header.deviceID], let key = devices[id],
              !devicesSignedOut.contains(id), let n = h[AppClient.Header.nonce], liveNonces.remove(n) != nil,
              let text = try? CanonicalText.request(method: method, path: path, body: body, nonce: n, deviceID: id) else {
            return false
        }
        return verify(key, h[AppClient.Header.signature], text)
    }

    /// Another client's bind (the browser's consent, a second phone): the binding moves.
    func bindElsewhere() {
        challenge = (UUID().uuidString.lowercased(), Base64URL.encode(Data(repeating: 7, count: 32)), Data([4]))
    }

    /// Registers a device directly, as if activated earlier.
    func register(_ key: DeviceSigner) -> String {
        let id = UUID().uuidString.lowercased()
        devices[id] = key.publicPoint
        return id
    }

    private func verify(_ point: Data, _ sig: String?, _ text: Data) -> Bool {
        guard let s = sig, let der = Base64URL.decode(s),
              let pub = try? P256.Signing.PublicKey(x963Representation: point),
              let signature = try? P256.Signing.ECDSASignature(derRepresentation: der) else {
            return false
        }
        return pub.isValidSignature(signature, for: text)
    }

    private func json(_ d: Data) -> [String: Any] {
        (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:]
    }

    private func fields(_ d: Data) -> [String] {
        json(d).keys.sorted()
    }

    private func reply(_ status: Int, object: Any? = nil, error: String? = nil) -> MockURLProtocol.Reply {
        var body: Data?
        if let o = object {
            body = try? JSONSerialization.data(withJSONObject: o)
        } else if let e = error {
            body = Data("{\"error\":\"\(e)\"}\n".utf8)
        }
        var headers = ["Date": httpDate(now), "Cache-Control": "no-store"]
        if body != nil {
            headers["Content-Type"] = "application/json; charset=utf-8"
        }
        return MockURLProtocol.Reply(status: status, body: body, headers: headers)
    }
}
