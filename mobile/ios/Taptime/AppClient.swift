import Foundation

// THE /app/v1 CLIENT (M16-07's wire contract, docs/plan/m16-mobil.md "Tel sözleşmesi"). One
// surface, the browser flow's twin: activate/lookup, activate/bind, activate/complete, and the
// signed requests every later screen uses (nonce, me, revoke; tap/start and tap are M16-17's).
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

struct NonceResponse: Decodable, Equatable {
    let nonce: String
    let expiresAt: String
}

struct MeResponse: Decodable, Equatable {
    let employeeName: String
    let brand: AppBrand
    let direction: String
}

private struct LookupRequest: Encodable {
    let code: String
}

private struct BindRequest: Encodable {
    let code: String
    let consent: Bool
    let publicKey: String
}

private struct CompleteRequest: Encodable {
    let code: String
    let publicKey: String
    let platform: String
    let sun: String
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
        static let nonce = "/app/v1/nonce"
        static let me = "/app/v1/me"
        static let revoke = "/app/v1/revoke"
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
        let body = try encode(CompleteRequest(code: code, publicKey: Base64URL.encode(signer.publicPoint),
                                              platform: AppClient.platform, sun: sun))
        let text: Data
        do {
            text = try CanonicalText.request(method: "POST", path: Path.complete, body: body,
                                             nonce: binding.challenge, deviceID: binding.activationId)
        } catch {
            throw AppAPIError.malformed
        }
        let signature = Base64URL.encode(try signer.sign(text))
        let (data, resp) = try await send("POST", Path.complete, body: body, headers: [
            Header.deviceID: binding.activationId,
            Header.nonce: binding.challenge,
            Header.signature: signature,
        ])
        let r: CompleteResponse = try decode(data, resp)
        guard CanonicalText.validDeviceID(r.deviceId) else {
            throw AppAPIError.unexpected(status: resp.statusCode)
        }
        return r
    }

    // MARK: Signed requests

    func me(_ device: DeviceIdentity) async throws -> MeResponse {
        let (data, resp) = try await signedRead("GET", Path.me, body: Data(), device: device)
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
