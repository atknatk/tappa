import Foundation

// Unpadded base64url (RFC 4648 §5), the one spelling the /app/v1 surface uses for every binary
// value: the public key, the nonce, the challenge and the signature (M16-07 wire contract).
//
// The decoder is STRICT the way the server's is: it accepts only the string its own encoder
// would write for the decoded bytes. Foundation's decoder takes padding, ignores nothing it
// should not and is lenient about the spare bits of the last character, so the check is the
// round trip, as internal/appdevice's ParseNonce does it.
enum Base64URL {
    static func encode(_ data: Data) -> String {
        var s = data.base64EncodedString()
        s = s.replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
        while s.hasSuffix("=") {
            s.removeLast()
        }
        return s
    }

    static func decode(_ s: String) -> Data? {
        guard !s.isEmpty, s.utf8.allSatisfy(isAlphabet) else {
            return nil
        }
        var std = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        let rem = std.count % 4
        if rem == 1 {
            return nil
        }
        if rem > 0 {
            std += String(repeating: "=", count: 4 - rem)
        }
        guard let data = Data(base64Encoded: std), encode(data) == s else {
            return nil
        }
        return data
    }

    private static func isAlphabet(_ c: UInt8) -> Bool {
        switch c {
        case UInt8(ascii: "A")...UInt8(ascii: "Z"), UInt8(ascii: "a")...UInt8(ascii: "z"),
             UInt8(ascii: "0")...UInt8(ascii: "9"), UInt8(ascii: "-"), UInt8(ascii: "_"):
            return true
        default:
            return false
        }
    }
}
