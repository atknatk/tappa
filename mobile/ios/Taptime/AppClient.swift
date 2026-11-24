import Foundation

// THE /app/v1 CLIENT (M16-07's wire contract, docs/plan/m16-mobil.md "Tel sözleşmesi"). One
// surface, the browser flow's twin: activate/lookup, activate/bind, activate/complete, and the
// signed requests every later screen uses (nonce, me, tap/start, tap, revoke). M16-23 adds the
// phone that cannot tap: complete with a venue screen's live QR (`liveQR` in place of `sun`), and
// the manager's approval (activate/approval, activate/approval/status), signed like complete.
//
// THE SERVER DECODES STRICTLY: an unknown field is a 400 (DisallowUnknownFields), and it matches
// field names case-insensitively. So every request body here is a struct with exactly the
// contract's fields, spelled as the table spells them; an optional the request leaves out is
// omitted, never sent as null. Answers are decoded leniently (an extra field the server adds
// later is ignored), which is the direction the versioning rule allows.
//
// THE BODY IS SIGNED AS SENT. The digest in the canonical text is over the exact bytes handed
// to URLSession -- the encoder's output is never re-encoded between signing and sending.
//
// NOTHING HERE LOGS. A request carries an invite code, a public key, a nonce, a challenge and a
// signature; none of them may reach a log line (CLAUDE.md §7), and the simplest way to keep that
// true is a client with no log call at all.

// MARK: - Wire types

struct AppBrand: Decodable, Equatable {
    struct Logo: Decodable, Equatable {
        let sha256: String
        let width: Int
        let height: Int
    }

    let name: String?
    let logo: Logo?
    /// Six hex digits without '#', already through the server's WCAG gate; nil is Taptime's look.
    let accent: String?

    static let taptime = AppBrand(name: nil, logo: nil, accent: nil)
}

struct LookupResponse: Decodable, Equatable {
    let employeeName: String
    let businessName: String
    let locationName: String?
    let wifiSsid: String?
    let secondDevice: Bool
    let brand: AppBrand
    /// M16-23: the manager-approval path is offered (no live QR screen runs at the person's
    /// workplace). Absent -- an older server -- is false.
    var managerApproval: Bool? = nil
}

struct BindResponse: Decodable, Equatable {
    /// App-Device-Id of the complete request. Not a device id: no device exists yet.
    let activationId: String
    /// App-Nonce of the complete request: the server's challenge the bound key signs over.
    let challenge: String
}

struct CompleteResponse: Decodable, Equatable {
    /// App-Device-Id of every later request.
    let deviceId: String
    let employeeName: String
}

/// The open manager-approval request (M16-23): the six digits the manager matches in the panel and
/// when the request runs out. The code is a credential of the moment: never printed (§7).
struct PendingApproval: Equatable, CustomStringConvertible, CustomDebugStringConvertible {
    let pairingCode: String
    let expiresAt: String

    /// "482913" -> "482 913", as the waiting screen shows it.
    var grouped: String {
        pairingCode.count == 6 ? String(pairingCode.prefix(3)) + " " + String(pairingCode.suffix(3)) : pairingCode
    }

    var description: String { "PendingApproval(pairingCode: <redacted>, expiresAt: \(expiresAt))" }
    var debugDescription: String { description }
}

/// activate/approval and approval/status's answer. `approved` IS a finished activation: the server
/// created the device in that very call, exactly as a successful complete.
enum ApprovalState: Equatable {
    case waiting(PendingApproval)
    case approved(CompleteResponse)
    case rejected
    case expired
}

private struct ApprovalResponse: Decodable {
    let state: String
    let pairingCode: String?
    let expiresAt: String?
    let deviceId: String?
    let employeeName: String?
}

struct NonceResponse: Decodable, Equatable {
    let nonce: String
    let expiresAt: String
}

struct MeResponse: Decodable, Equatable {
    let employeeName: String
    let brand: AppBrand
    let direction: String
    // The staff app's facts (M16-43/45/30/37). An older server omits them: nil, read as none / 0.
    var presence: Presence?
    var shift: UsualShift?
    var missingTimes: Int?
    var hoursWeeks: Int?
    var businessName: String?

    /// The employer's name: businessName, else the brand's caption.
    var business: String { (businessName?.isEmpty == false ? businessName : brand.name) ?? "" }
}

/// An open check-in the next tap would close (M16-45): RFC 3339 instants for the phone's
/// scheduling, the business's wall clocks for the words. The app converts no zone (§6).
struct Presence: Decodable, Equatable {
    let inSince: String
    let inSinceLocal: String
    let shiftEndsAt: String?
    let shiftEndsAtLocal: String?
    let remindAt: String?
    let showUntil: String
    let staleAt: String

    static func instant(_ s: String?) -> Date? {
        guard let s = s else { return nil }
        let f = ISO8601DateFormatter()
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f.date(from: s)
    }
}

/// The usual shift (only when the owner marked shifts real, 00116).
struct UsualShift: Decodable, Equatable {
    let start: String
    let end: String
    let overnight: Bool
    let venue: String
}

/// "Missing times" (M16-43): the records that take a claim now and the person's own claims.
struct ClaimsResponse: Decodable, Equatable {
    struct Bound: Decodable, Equatable {
        let at: String
        let inclusive: Bool
    }
    struct Open: Decodable, Equatable, CustomStringConvertible {
        let context: String
        let kind: String
        let anchorDate: String
        let anchorAt: String
        let day: String
        let windowFrom: Bound
        let windowTo: Bound
        let prefill: String?
        // The context is signed for this device: never printed (§7).
        var description: String { "Open(kind: \(kind), day: \(day))" }
    }
    let open: [Open]
}

struct ClaimSendResponse: Decodable, Equatable {
    let result: String
}

/// My hours (M16-31): one week, minutes whole and floored by the server.
struct HoursResponse: Decodable, Equatable {
    struct Totals: Decodable, Equatable {
        let workedMinutes: Int
        let awaitingMinutes: Int
        let openEntries: Int
        let checkInMissing: Int
        let truncated: Bool
        let complete: Bool
    }
    struct Day: Decodable, Equatable {
        let date: String
        let workedMinutes: Int
    }
    struct Record: Decodable, Equatable {
        let date: String
        let at: String
        let venue: String
        let direction: String
        let channel: String
        let state: String
        let claim: String?
    }
    struct Waiting: Decodable, Equatable {
        let records: Int
        let claims: Int
    }
    let week: String
    let weeksVisible: Int
    let hasPrev: Bool
    let hasNext: Bool
    let totals: Totals
    let days: [Day]
    let records: [Record]
    let nextCursor: String?
    let waiting: Waiting?
}

/// GET /t's twin: the tap screen of THIS plaque read. Nothing was recorded and no counter moved.
struct TapStartResponse: Decodable, Equatable {
    let employeeName: String
    /// The TAPPED venue, "" for another business's plaque (the screen then has no venue line).
    let locationName: String
    let brand: AppBrand
    /// "in", "out", or "" for the direction-neutral "Tap".
    let direction: String
    let qr: Bool
    /// The signed tap context POST /app/v1/tap carries back, bound to this device.
    let context: String
}

/// POST /api/checkin's answer, as the result screen draws it.
struct TapResponse: Decodable, Equatable {
    let outcome: String
    /// ok / flag / reject / ignored.
    let verdict: String
    let direction: String
    let venue: String
    /// HH:MM in the business's time zone, already formatted by the server.
    let at: String
    let businessType: String
    let practice: Bool?
}

private struct LookupRequest: Encodable {
    let code: String
}

private struct BindRequest: Encodable {
    let code: String
    let consent: Bool
    let publicKey: String
}

/// complete's body: EXACTLY ONE of the plaque's `sun` and a venue screen's `liveQR` (M16-23). The
/// synthesised Encodable omits the nil one.
private struct CompleteRequest: Encodable {
    let code: String
    let publicKey: String
    let platform: String
    var sun: String?
    var liveQR: String?
}

/// activate/approval and approval/status (M16-23): complete's fields without the read.
private struct ApprovalRequest: Encodable {
    let code: String
    let publicKey: String
    let platform: String
}

/// tap/start's body: exactly ONE of the two (M16-22). The synthesised Encodable omits a nil
/// optional (encodeIfPresent), so a plaque read sends {"sun"} and a live QR {"liveQR"}.
private struct TapStartRequest: Encodable {
    var sun: String?
    var liveQR: String?
}

/// The press: the signed context, the direction pressed, and at most one fix with its mock report.
/// Every optional the press does not have is OMITTED (synthesised Encodable writes nil optionals
/// with encodeIfPresent), so "no fix" is no lat/lng at all, never a null.
struct TapRequest: Encodable, Equatable {
    let context: String
    let want: String?
    let lat: Double?
    let lng: Double?
    let mockLocation: Bool?
}

private struct ErrorBody: Decodable {
    let error: String
}

// MARK: - Errors

/// The closed error vocabulary of the surface (internal/handler/app.go appErrors): one status
/// per code.
enum AppErrorCode: String, CaseIterable {
    case badRequest = "bad_request"
    case invalidInvite = "invalid_invite"
    case consentRequired = "consent_required"
    case invalidKey = "invalid_key"
    case activationNeedsTouch = "activation_needs_touch"
    case activationTapFailed = "activation_tap_failed"
    case staleContext = "stale_context"
    case foreignTenant = "foreign_tenant"
    case unknownPlaque = "unknown_plaque"
    case notFound = "not_found"
    case methodNotAllowed = "method_not_allowed"
    case newKeyRequired = "new_key_required"
    case activationNotReady = "activation_not_ready"
    /// M16-23: a live QR screen runs at the workplace -- scan it; no manager approval.
    case screenAvailable = "screen_available"
    case tryAgain = "try_again"
    case rateLimited = "rate_limited"
    case serverError = "server_error"
}

enum AppAPIError: Error, Equatable {
    /// 401 with an empty body: the signature was refused. For a signed read and a nonce'd request
    /// this is reported only after the one retry with a fresh clock measurement.
    case unauthenticated
    /// A code from the closed vocabulary; retryAfter is the 429's Retry-After, in seconds.
    case refused(AppErrorCode, retryAfter: Int?)
    /// A status or body outside the contract.
    case unexpected(status: Int)
    /// The request never got an HTTP answer (offline, DNS, TLS, timeout).
    case transport
    /// The device's own parts cannot form a valid request (CanonicalText refused them).
    case malformed
}

// MARK: - Client

/// The device as the server knows it after activation: its id and the key that signs for it.
struct DeviceIdentity {
    let deviceID: String
    let signer: DeviceSigner
}

final class AppClient {
    enum Path {
        static let lookup = "/app/v1/activate/lookup"
        static let bind = "/app/v1/activate/bind"
        static let complete = "/app/v1/activate/complete"
        static let approval = "/app/v1/activate/approval"
        static let approvalStatus = "/app/v1/activate/approval/status"
        static let nonce = "/app/v1/nonce"
        static let me = "/app/v1/me"
        static let tapStart = "/app/v1/tap/start"
        static let tap = "/app/v1/tap"
        static let revoke = "/app/v1/revoke"
        // The staff app's own screens (M16-43, M16-31, M16-37).
        static let claims = "/app/v1/claims"
        static let claimsSend = "/app/v1/claims/send"
        static let hours = "/app/v1/hours"
        static let accountDelete = "/app/v1/account/delete"
    }

    enum Header {
        static let deviceID = "App-Device-Id"
        static let nonce = "App-Nonce"
        static let timestamp = "App-Timestamp"
        static let signature = "App-Signature"
    }

    /// The platform label app_devices.platform accepts for this app.
    static let platform = "ios"

    let baseURL: URL
    let clock: ServerClock
    private let session: URLSession
    private let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.outputFormatting = [.withoutEscapingSlashes]
        return e
    }()

    init(baseURL: URL, session: URLSession = AppClient.makeSession(), clock: ServerClock = ServerClock()) {
        self.baseURL = baseURL
        self.session = session
        self.clock = clock
    }

    /// An ephemeral session: no cache (every answer is no-store anyway), no cookies -- the app's
    /// identity is the key, and a cookie jar would be a second, unsigned one -- and NO REDIRECTS
    /// (RedirectRefusal): the surface never redirects, so a 3xx is somebody else's answer.
    static func makeSession(protocolClasses: [AnyClass]? = nil) -> URLSession {
        let c = URLSessionConfiguration.ephemeral
        c.requestCachePolicy = .reloadIgnoringLocalCacheData
        c.urlCache = nil
        c.httpCookieStorage = nil
        c.httpShouldSetCookies = false
        c.httpCookieAcceptPolicy = .never
        c.timeoutIntervalForRequest = 20
        if let p = protocolClasses {
            c.protocolClasses = p
        }
        return URLSession(configuration: c, delegate: RedirectRefusal(), delegateQueue: nil)
    }

    // MARK: Activation

    func lookup(code: String) async throws -> LookupResponse {
        let body = try encode(LookupRequest(code: code))
        let (data, resp) = try await send("POST", Path.lookup, body: body, headers: [:])
        return try decode(data, resp)
    }

    /// Records the consent bound to this key. The person pressed "Agree and continue": consent is
    /// always true here, the screen is the only caller.
    func bind(code: String, publicPoint: Data) async throws -> BindResponse {
        let body = try encode(BindRequest(code: code, consent: true, publicKey: Base64URL.encode(publicPoint)))
        let (data, resp) = try await send("POST", Path.bind, body: body, headers: [:])
        let r: BindResponse = try decode(data, resp)
        guard CanonicalText.validDeviceID(r.activationId), CanonicalText.validNonce(r.challenge) else {
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
        return r
    }

    /// The first genuine NFC read, signed by the BOUND key over the server's challenge. No retry:
    /// the text carries no clock, so a fresh measurement could not change the answer.
    func complete(code: String, signer: DeviceSigner, binding: BindResponse, sun: String) async throws -> CompleteResponse {
        try await complete(CompleteRequest(code: code, publicKey: Base64URL.encode(signer.publicPoint),
                                           platform: AppClient.platform, sun: sun), signer: signer, binding: binding)
    }

    /// M16-23: a venue screen's live QR in place of the plaque -- the /q URL's query as `liveQR`.
    func complete(code: String, signer: DeviceSigner, binding: BindResponse, liveQR: String) async throws -> CompleteResponse {
        try await complete(CompleteRequest(code: code, publicKey: Base64URL.encode(signer.publicPoint),
                                           platform: AppClient.platform, liveQR: liveQR), signer: signer, binding: binding)
    }

    private func complete(_ request: CompleteRequest, signer: DeviceSigner, binding: BindResponse) async throws -> CompleteResponse {
        let (data, resp) = try await activationSigned(Path.complete, body: try encode(request), signer: signer, binding: binding)
        let r: CompleteResponse = try decode(data, resp)
        guard CanonicalText.validDeviceID(r.deviceId) else {
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
        return r
    }

    /// M16-23: "My phone can't tap" -- opens a manager-approval request (an earlier open one of the
    /// invitation is withdrawn by the server).
    func approval(code: String, signer: DeviceSigner, binding: BindResponse) async throws -> ApprovalState {
        try await approval(Path.approval, code: code, signer: signer, binding: binding)
    }

    /// M16-23: the request's state. `approved` means the device was created in this call.
    func approvalStatus(code: String, signer: DeviceSigner, binding: BindResponse) async throws -> ApprovalState {
        try await approval(Path.approvalStatus, code: code, signer: signer, binding: binding)
    }

    private func approval(_ path: String, code: String, signer: DeviceSigner, binding: BindResponse) async throws -> ApprovalState {
        let body = try encode(ApprovalRequest(code: code, publicKey: Base64URL.encode(signer.publicPoint), platform: AppClient.platform))
        let (data, resp) = try await activationSigned(path, body: body, signer: signer, binding: binding)
        let r: ApprovalResponse = try decode(data, resp)
        switch r.state {
        case "waiting":
            guard let c = r.pairingCode, c.utf8.count == 6, c.utf8.allSatisfy({ (48...57).contains($0) }) else {
                throw AppAPIError.unexpected(status: resp.statusCode)
            }
            return .waiting(PendingApproval(pairingCode: c, expiresAt: r.expiresAt ?? ""))
        case "approved":
            guard let id = r.deviceId, CanonicalText.validDeviceID(id), let name = r.employeeName else {
                throw AppAPIError.unexpected(status: resp.statusCode)
            }
            return .approved(CompleteResponse(deviceId: id, employeeName: name))
        case "rejected":
            return .rejected
        case "expired":
            return .expired
        default:
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
    }

    /// The activation signature: App-Device-Id = the activation id, App-Nonce = the server's
    /// challenge, signed by the BOUND key over `path` -- each route its own text. Not retried.
    private func activationSigned(_ path: String, body: Data, signer: DeviceSigner, binding: BindResponse) async throws -> (Data, HTTPURLResponse) {
        let text: Data
        do {
            text = try CanonicalText.request(method: "POST", path: path, body: body,
                                             nonce: binding.challenge, deviceID: binding.activationId)
        } catch {
            throw AppAPIError.malformed
        }
        let signature = Base64URL.encode(try signer.sign(text))
        return try await send("POST", path, body: body, headers: [
            Header.deviceID: binding.activationId,
            Header.nonce: binding.challenge,
            Header.signature: signature,
        ])
    }

    // MARK: Signed requests

    func me(_ device: DeviceIdentity) async throws -> MeResponse {
        let (data, resp) = try await signedRead("GET", Path.me, body: Data(), device: device)
        return try decode(data, resp)
    }

    /// The plaque read's screen: a signed read (nothing recorded, no counter spent), retried once on
    /// a 401 like every signed read.
    func tapStart(_ device: DeviceIdentity, sun: String) async throws -> TapStartResponse {
        try await tapStart(device, body: TapStartRequest(sun: sun))
    }

    /// A venue screen's live QR read (M16-22): the /q URL's query as `liveQR` -- GET /q's twin, the
    /// same signed read, nothing recorded.
    func tapStart(_ device: DeviceIdentity, liveQR: String) async throws -> TapStartResponse {
        try await tapStart(device, body: TapStartRequest(liveQR: liveQR))
    }

    /// One of the two reads opened by a link: plaque or live QR.
    func tapStart(_ device: DeviceIdentity, link: TapLink) async throws -> TapStartResponse {
        switch link {
        case .plaque(let sun):
            return try await tapStart(device, sun: sun)
        case .liveQR(let q):
            return try await tapStart(device, liveQR: q)
        }
    }

    private func tapStart(_ device: DeviceIdentity, body request: TapStartRequest) async throws -> TapStartResponse {
        let body = try encode(request)
        let (data, resp) = try await signedRead("POST", Path.tapStart, body: body, device: device)
        let r: TapStartResponse = try decode(data, resp)
        guard !r.context.isEmpty else {
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
        return r
    }

    /// THE APP'S ONE WRITER. Nonce-signed; a 401 is retried once with a new nonce (a refused
    /// signature records nothing). Any other failure is NOT retried here: an answer lost on the way
    /// back may belong to a recorded tap, and only a new physical read may follow it.
    func tap(_ device: DeviceIdentity, _ press: TapRequest) async throws -> TapResponse {
        let body = try encode(press)
        let (data, resp) = try await signedWithNonce("POST", Path.tap, body: body, device: device)
        return try decode(data, resp)
    }

    func nonce(_ device: DeviceIdentity) async throws -> NonceResponse {
        let (data, resp) = try await signedRead("POST", Path.nonce, body: Data(), device: device)
        let r: NonceResponse = try decode(data, resp)
        guard CanonicalText.validNonce(r.nonce) else {
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
        return r
    }

    // MARK: The staff app's own screens

    func claims(_ device: DeviceIdentity) async throws -> ClaimsResponse {
        let (data, resp) = try await signedRead("POST", Path.claims, body: Data("{}".utf8), device: device)
        return try decode(data, resp)
    }

    /// One claim: the server's result word ("sent", "already_sent", "outside_window", …).
    func claimSend(_ device: DeviceIdentity, context: String, at: String) async throws -> String {
        let body = try encode(["context": context, "at": at])
        let (data, resp) = try await signedWithNonce("POST", Path.claimsSend, body: body, device: device)
        let r: ClaimSendResponse = try decode(data, resp)
        return r.result
    }

    /// My hours: nil week is this week; `after` the previous page's cursor. 404 = turned off.
    func hours(_ device: DeviceIdentity, week: String?, after: String?) async throws -> HoursResponse {
        var fields: [String: String] = [:]
        if let w = week { fields["week"] = w }
        if let a = after { fields["after"] = a }
        let body = try encode(fields)
        let (data, resp) = try await signedRead("POST", Path.hours, body: body, device: device)
        return try decode(data, resp)
    }

    /// "Delete my account" (M16-37): signed out everywhere, the employer told; 204.
    func deleteAccount(_ device: DeviceIdentity) async throws {
        let (data, resp) = try await signedWithNonce("POST", Path.accountDelete, body: Data("{}".utf8), device: device)
        guard resp.statusCode == 204 else {
            if !(200...299).contains(resp.statusCode) {
                let _: ErrorBody = try decode(data, resp) // throws the contract's error
            }
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
    }

    /// The device's own sign-out: 204, nothing else.
    func revoke(_ device: DeviceIdentity) async throws {
        let (data, resp) = try await signedWithNonce("POST", Path.revoke, body: Data(), device: device)
        guard resp.statusCode == 204 else {
            if !(200...299).contains(resp.statusCode) {
                let _: ErrorBody = try decode(data, resp) // throws the contract's error
            }
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
    }

    /// A signed read: the timestamp is the SERVER-CORRECTED second. A 401 is retried exactly once,
    /// re-signed after the 401's own Date header has re-measured the clock (send() observes every
    /// answer); a second 401 is the signed-out answer.
    func signedRead(_ method: String, _ path: String, body: Data, device: DeviceIdentity) async throws -> (Data, HTTPURLResponse) {
        for attempt in 0..<2 {
            let ts = CanonicalText.timestamp(clock.now())
            let text: Data
            do {
                text = try CanonicalText.read(method: method, path: path, body: body, timestamp: ts, deviceID: device.deviceID)
            } catch {
                throw AppAPIError.malformed
            }
            let signature = Base64URL.encode(try device.signer.sign(text))
            let (data, resp) = try await send(method, path, body: body, headers: [
                Header.deviceID: device.deviceID,
                Header.timestamp: ts,
                Header.signature: signature,
            ])
            if resp.statusCode == 401, attempt == 0 {
                continue
            }
            return (data, resp)
        }
        throw AppAPIError.unauthenticated // unreachable: the second pass always returns
    }

    /// A state-changing request: a fresh nonce (itself a signed read), then the request signed
    /// over it. A 401 is retried once with a NEW nonce fetched under a fresh clock measurement --
    /// safe, because a refused signature records nothing (M16-07).
    func signedWithNonce(_ method: String, _ path: String, body: Data, device: DeviceIdentity) async throws -> (Data, HTTPURLResponse) {
        for attempt in 0..<2 {
            let issued = try await nonce(device)
            let text: Data
            do {
                text = try CanonicalText.request(method: method, path: path, body: body, nonce: issued.nonce, deviceID: device.deviceID)
            } catch {
                throw AppAPIError.malformed
            }
            let signature = Base64URL.encode(try device.signer.sign(text))
            let (data, resp) = try await send(method, path, body: body, headers: [
                Header.deviceID: device.deviceID,
                Header.nonce: issued.nonce,
                Header.signature: signature,
            ])
            if resp.statusCode == 401, attempt == 0 {
                continue
            }
            return (data, resp)
        }
        throw AppAPIError.unauthenticated
    }

    // MARK: Transport

    /// One exchange. EVERY answer's Date header feeds the clock, whatever its status.
    func send(_ method: String, _ path: String, body: Data, headers: [String: String]) async throws -> (Data, HTTPURLResponse) {
        guard var comps = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            throw AppAPIError.malformed
        }
        comps.percentEncodedPath = path
        comps.percentEncodedQuery = nil
        guard let url = comps.url else {
            throw AppAPIError.malformed
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        if method != "GET" {
            req.httpBody = body
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        for (k, v) in headers {
            req.setValue(v, forHTTPHeaderField: k)
        }
        let sentAt = clock.local()
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: req)
        } catch {
            throw AppAPIError.transport
        }
        guard let http = response as? HTTPURLResponse else {
            throw AppAPIError.transport
        }
        // A redirect is not followed (RedirectRefusal) and not read: no /app/v1 route answers
        // 3xx, so it comes from something in between (a captive portal, a misrouted proxy) and
        // neither its body nor its Date header is the server's.
        if (300...399).contains(http.statusCode) {
            throw AppAPIError.unexpected(status: http.statusCode)
        }
        clock.observe(dateHeader: http.value(forHTTPHeaderField: "Date"), sentAt: sentAt, receivedAt: clock.local())
        return (data, http)
    }

    private func encode<T: Encodable>(_ v: T) throws -> Data {
        do {
            return try encoder.encode(v)
        } catch {
            throw AppAPIError.malformed
        }
    }

    /// A 2xx body as T; anything else as the contract's error.
    func decode<T: Decodable>(_ data: Data, _ resp: HTTPURLResponse) throws -> T {
        switch resp.statusCode {
        case 200...299:
            guard let v = try? JSONDecoder().decode(T.self, from: data) else {
                throw AppAPIError.unexpected(status: resp.statusCode)
            }
            return v
        case 401:
            throw AppAPIError.unauthenticated
        default:
            guard let e = try? JSONDecoder().decode(ErrorBody.self, from: data),
                  let code = AppErrorCode(rawValue: e.error) else {
                throw AppAPIError.unexpected(status: resp.statusCode)
            }
            let retry = resp.value(forHTTPHeaderField: "Retry-After").flatMap { Int($0) }
            throw AppAPIError.refused(code, retryAfter: retry)
        }
    }
}

/// Refuses every HTTP redirect (M16-16 review). Following one would re-send a signed request --
/// its body, its signature, a nonce -- to whatever URL a 3xx names; the server's routes never
/// redirect, so the answer that asks for it is treated as a problem, not obeyed.
final class RedirectRefusal: NSObject, URLSessionTaskDelegate {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}
