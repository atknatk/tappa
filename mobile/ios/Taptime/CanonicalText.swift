import CryptoKit
import Foundation

// THE SIGNED BYTES (ADR 0045 §2.3; M16-05, M16-07). The server's definition is
// internal/appdevice/canonical.go; this is its twin, held to the same bytes by the known-answer
// vectors in test/fixtures/appdevice_vectors.json, which the test target copies in unchanged.
//
// Six lines, each ended by one LF:
//
//     tappa-app-request-v1 | tappa-app-read-v1     the prefix: a state-changing request, or a read
//     POST                                         the method, upper case
//     /app/v1/tap                                  the path as sent, no query
//     <hex SHA-256 of the raw body bytes>          lower case, 64 characters
//     <nonce as issued> | <decimal Unix second>    the freshness line
//     <device id, canonical lower-case uuid>
//
// Every field is checked against the closed character set the server accepts BEFORE it is
// written, so no field can carry an LF and two requests can never share a text. A part the
// server would refuse is refused here too (CanonicalError.malformed): the app finds out from a
// unit test, not from a 401 it cannot tell apart from a signed-out phone.
enum CanonicalText {
    static let requestPrefix = "tappa-app-request-v1"
    static let readPrefix = "tappa-app-read-v1"

    enum CanonicalError: Error, Equatable {
        case malformed
    }

    /// The canonical text of a state-changing request, signed over a single-use nonce.
    static func request(method: String, path: String, body: Data, nonce: String, deviceID: String) throws -> Data {
        guard validMethod(method), validPath(path), validNonce(nonce), validDeviceID(deviceID) else {
            throw CanonicalError.malformed
        }
        return build(requestPrefix, method, path, body, nonce, deviceID)
    }

    /// The canonical text of a signed read (GET /app/v1/me, POST /app/v1/tap/start, POST
    /// /app/v1/nonce), signed over the server-corrected Unix second.
    static func read(method: String, path: String, body: Data, timestamp: String, deviceID: String) throws -> Data {
        guard validMethod(method), validPath(path), validTimestamp(timestamp), validDeviceID(deviceID) else {
            throw CanonicalError.malformed
        }
        return build(readPrefix, method, path, body, timestamp, deviceID)
    }

    /// The App-Timestamp spelling of a second: decimal, no sign, no leading zero (the server's
    /// strconv.FormatInt). Seconds are truncated, never rounded up into the future.
    static func timestamp(_ date: Date) -> String {
        String(Int64(date.timeIntervalSince1970.rounded(.down)))
    }

    static func hexSHA256(_ data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    private static func build(_ prefix: String, _ method: String, _ path: String, _ body: Data,
                              _ freshness: String, _ deviceID: String) -> Data {
        var text = ""
        for field in [prefix, method, path, hexSHA256(body), freshness, deviceID] {
            text += field
            text += "\n"
        }
        return Data(text.utf8)
    }

    // 1..16 upper-case ASCII letters.
    static func validMethod(_ m: String) -> Bool {
        let b = Array(m.utf8)
        return (1...16).contains(b.count) && b.allSatisfy { $0 >= UInt8(ascii: "A") && $0 <= UInt8(ascii: "Z") }
    }

    // Starts with '/', 1..1024 bytes of visible ASCII, no '?' and no '#'.
    static func validPath(_ p: String) -> Bool {
        let b = Array(p.utf8)
        guard (1...1024).contains(b.count), b[0] == UInt8(ascii: "/") else {
            return false
        }
        return b.allSatisfy { $0 >= 0x21 && $0 <= 0x7E && $0 != UInt8(ascii: "?") && $0 != UInt8(ascii: "#") }
    }

    // 43 characters of unpadded base64url that decode to 32 bytes and re-encode to themselves.
    static func validNonce(_ n: String) -> Bool {
        n.utf8.count == 43 && Base64URL.decode(n)?.count == 32
    }

    // 1..19 ASCII digits, the first not '0'.
    static func validTimestamp(_ t: String) -> Bool {
        let b = Array(t.utf8)
        guard (1...19).contains(b.count), b[0] != UInt8(ascii: "0") else {
            return false
        }
        return b.allSatisfy { $0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9") }
    }

    // The canonical lower-case hyphenated form only, and never the nil uuid: one device, one text.
    static func validDeviceID(_ s: String) -> Bool {
        guard s.utf8.count == 36, let id = UUID(uuidString: s) else {
            return false
        }
        return id.uuidString.lowercased() == s && s != "00000000-0000-0000-0000-000000000000"
    }
}
