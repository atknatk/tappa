import CryptoKit
import XCTest
@testable import Taptime

// M16-05's KNOWN-ANSWER VECTORS (test/fixtures/appdevice_vectors.json), read from the repository
// file itself: the test target copies it in at build time, so Go, Kotlin and Swift are held to
// ONE byte string and nobody keeps a second copy. The signatures were made by OpenSSL, not by
// any of the three implementations.
final class AppDeviceVectorTests: XCTestCase {
    private var v: [String: Any] = [:]

    override func setUpWithError() throws {
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "appdevice_vectors", withExtension: "json"),
                                "the fixture is not in the test bundle")
        v = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
    }

    private func list(_ k: String) -> [[String: Any]] { v[k] as? [[String: Any]] ?? [] }

    private func hex(_ s: String) -> Data {
        var d = Data()
        var i = s.startIndex
        while i < s.endIndex {
            let j = s.index(i, offsetBy: 2)
            d.append(UInt8(s[i..<j], radix: 16)!)
            i = j
        }
        return d
    }

    private func key(_ name: String) throws -> P256.Signing.PublicKey {
        let keys = try XCTUnwrap(v["keys"] as? [String: [String: String]])
        return try P256.Signing.PublicKey(x963Representation: hex(try XCTUnwrap(keys[name]?["public_point_hex"])))
    }

    private func requestText(_ r: [String: Any]) throws -> Data {
        try CanonicalText.request(method: r["method"] as! String, path: r["path"] as! String,
                                  body: Data((r["body_utf8"] as! String).utf8),
                                  nonce: r["nonce"] as! String, deviceID: r["device_id"] as! String)
    }

    private func readText(_ r: [String: Any]) throws -> Data {
        try CanonicalText.read(method: r["method"] as! String, path: r["path"] as! String,
                               body: Data((r["body_utf8"] as! String).utf8),
                               timestamp: r["timestamp"] as! String, deviceID: r["device_id"] as! String)
    }

    func testTheFixtureHasEverySection() {
        XCTAssertEqual(list("requests").count, 4)
        XCTAssertEqual(list("signatures").count, 8)
        XCTAssertEqual(list("reads").count, 3)
        XCTAssertEqual(list("read_signatures").count, 6)
        XCTAssertEqual(list("malformed_requests").count, 12)
        XCTAssertEqual(list("invalid_public_keys").count, 7)
    }

    func testRequestCanonicalTextsAreByteForByte() throws {
        for r in list("requests") {
            let name = r["name"] as! String
            let text = try requestText(r)
            XCTAssertEqual(text, Data((r["canonical_text"] as! String).utf8), name)
            XCTAssertEqual(CanonicalText.hexSHA256(text), r["canonical_sha256_hex"] as? String, name)
            XCTAssertEqual(CanonicalText.hexSHA256(Data((r["body_utf8"] as! String).utf8)), r["body_sha256_hex"] as? String, name)
        }
    }

    func testReadCanonicalTextsAreByteForByte() throws {
        for r in list("reads") {
            let name = r["name"] as! String
            let text = try readText(r)
            XCTAssertEqual(text, Data((r["canonical_text"] as! String).utf8), name)
            XCTAssertEqual(CanonicalText.hexSHA256(text), r["canonical_sha256_hex"] as? String, name)
        }
    }

    /// Each OpenSSL signature verifies -- or does not -- over the text THIS builder makes, exactly
    /// as the fixture says: the app's text and the server's are one.
    func testRequestSignaturesVerifyAsTheFixtureSays() throws {
        let requests = Dictionary(uniqueKeysWithValues: list("requests").map { ($0["name"] as! String, $0) })
        for s in list("signatures") {
            let name = s["name"] as! String
            let text = try requestText(try XCTUnwrap(requests[s["request"] as! String]))
            let der = try XCTUnwrap(Base64URL.decode(s["signature_b64url"] as! String), name)
            XCTAssertEqual(Base64URL.encode(der), s["signature_b64url"] as? String, name)
            let sig = try P256.Signing.ECDSASignature(derRepresentation: der)
            XCTAssertEqual(try key(s["key"] as! String).isValidSignature(sig, for: text), s["valid"] as? Bool, name)
        }
    }

    func testReadSignaturesVerifyAsTheFixtureSays() throws {
        let reads = Dictionary(uniqueKeysWithValues: list("reads").map { ($0["name"] as! String, $0) })
        for s in list("read_signatures") {
            let name = s["name"] as! String
            let text = try readText(try XCTUnwrap(reads[s["read"] as! String]))
            let sig = try P256.Signing.ECDSASignature(derRepresentation: try XCTUnwrap(Base64URL.decode(s["signature_b64url"] as! String)))
            XCTAssertEqual(try key(s["key"] as! String).isValidSignature(sig, for: text), s["valid"] as? Bool, name)
        }
    }

    /// Every part the server refuses, the app refuses before signing.
    func testMalformedRequestsAreRefusedBeforeSigning() {
        for r in list("malformed_requests") {
            XCTAssertThrowsError(try CanonicalText.request(method: r["method"] as! String, path: r["path"] as! String,
                                                           body: Data(), nonce: r["nonce"] as! String,
                                                           deviceID: r["device_id"] as! String),
                                 r["name"] as! String)
        }
    }

    func testTheThreeKeysAreValidPointsAndTheInvalidOnesAreNot() throws {
        let keys = try XCTUnwrap(v["keys"] as? [String: [String: String]])
        for (name, k) in keys {
            XCTAssertTrue(DevicePublicKey.isValidPoint(hex(k["public_point_hex"]!)), name)
        }
        for k in list("invalid_public_keys") {
            XCTAssertFalse(DevicePublicKey.isValidPoint(hex(k["public_point_hex"] as! String)), k["name"] as! String)
        }
    }

    /// The signing path the flow tests use over a vector's text: the test target's software key,
    /// the same DER + base64url the Secure Enclave key emits (DeviceKeyPolicyTests signs with the
    /// real one where the run has a Secure Enclave).
    func testAnAppSignatureOverAVectorTextVerifies() throws {
        let k = SoftwareDeviceKey()
        let text = try requestText(list("requests")[0])
        let wire = Base64URL.encode(try k.sign(text))
        let sig = try P256.Signing.ECDSASignature(derRepresentation: try XCTUnwrap(Base64URL.decode(wire)))
        XCTAssertTrue(k.key.publicKey.isValidSignature(sig, for: text))
        XCTAssertEqual(k.publicPoint.count, 65)
        XCTAssertEqual(k.publicPoint.first, 0x04)
    }

    func testTimestampSpellingIsTheServers() {
        XCTAssertEqual(CanonicalText.timestamp(Date(timeIntervalSince1970: 1_794_700_800.999)), "1794700800")
        XCTAssertTrue(CanonicalText.validTimestamp("1794700800"))
        for bad in ["", "0", "01794700800", "+1794700800", "-1", "17947 00800", "12345678901234567890"] {
            XCTAssertFalse(CanonicalText.validTimestamp(bad), bad)
        }
        XCTAssertThrowsError(try CanonicalText.read(method: "GET", path: "/app/v1/me", body: Data(),
                                                    timestamp: "0179", deviceID: "7d1c2a4e-9b3f-4c5d-8e6f-0a1b2c3d4e5f"))
    }

    func testBase64URLIsStrict() {
        XCTAssertNil(Base64URL.decode("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="))
        XCTAssertNil(Base64URL.decode("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh9"))
        XCTAssertNil(Base64URL.decode("//79"))
        XCTAssertNil(Base64URL.decode(""))
        XCTAssertEqual(Base64URL.decode("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")?.count, 32)
    }
}
