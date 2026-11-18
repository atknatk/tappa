import CryptoKit
import Security
import XCTest
@testable import Taptime

// CLAUDE.md §4.1 and ADR 0045 §2.3: the device key's access control asks nothing of the person.
// The flags are checked as an ALLOW-LIST -- exactly privateKeyUsage, every other bit zero -- so a
// flag nobody thought to forbid fails too, and this file never has to spell the forbidden ones
// (the red-line scan reads test sources as well).
final class DeviceKeyPolicyTests: XCTestCase {
    func testTheFlagsAreExactlyPrivateKeyUsage() {
        XCTAssertEqual(DeviceKeyPolicy.flags, [.privateKeyUsage])
        XCTAssertEqual(DeviceKeyPolicy.flags.rawValue, SecAccessControlCreateFlags.privateKeyUsage.rawValue)
        // privateKeyUsage is one bit: nothing else rides along in the raw value.
        XCTAssertEqual(DeviceKeyPolicy.flags.rawValue.nonzeroBitCount, 1)
    }

    func testTheProtectionIsWhenUnlockedThisDeviceOnly() {
        XCTAssertEqual(DeviceKeyPolicy.protection as String, kSecAttrAccessibleWhenUnlockedThisDeviceOnly as String)
    }

    func testTheAccessControlIsCreatedFromThePolicy() throws {
        XCTAssertNoThrow(try DeviceKeyPolicy.accessControl())
    }

    /// No software fallback: where the Secure Enclave is missing, the production factory refuses
    /// -- it does not hand back some other key. Where it is present, the key it makes is the
    /// chip's: a valid P-256 point whose DER signatures verify over a vector's canonical text, a
    /// fresh pair per call, and a handle that restores the same key.
    ///
    /// MEASURED (2026-11-18): the iOS 26.3 simulator on this Mac answers isAvailable = true and
    /// the second branch runs; a simulator or device without one runs the first.
    func testTheFactoryMakesSecureEnclaveKeysOrNone() throws {
        let f = SecureEnclaveKeyFactory()
        XCTAssertEqual(f.isAvailable, SecureEnclave.isAvailable)
        guard SecureEnclave.isAvailable else {
            XCTAssertThrowsError(try f.newKey()) { XCTAssertEqual($0 as? DeviceKeyError, .secureEnclaveUnavailable) }
            XCTAssertThrowsError(try f.restore(Data(repeating: 1, count: 64))) {
                XCTAssertEqual($0 as? DeviceKeyError, .secureEnclaveUnavailable)
            }
            return
        }
        let a = try f.newKey()
        let b = try f.newKey()
        XCTAssertTrue(a is SecureEnclaveDeviceKey)
        XCTAssertTrue(DevicePublicKey.isValidPoint(a.publicPoint))
        XCTAssertNotEqual(a.publicPoint, b.publicPoint, "every activation gets a new pair")
        let text = try CanonicalText.read(method: "GET", path: "/app/v1/me", body: Data(), timestamp: "1794700800",
                                          deviceID: "7d1c2a4e-9b3f-4c5d-8e6f-0a1b2c3d4e5f")
        let der = try a.sign(text)
        let pub = try P256.Signing.PublicKey(x963Representation: a.publicPoint)
        XCTAssertTrue(pub.isValidSignature(try P256.Signing.ECDSASignature(derRepresentation: der), for: text))
        let again = try f.restore(a.persistentHandle)
        XCTAssertEqual(again.publicPoint, a.publicPoint)
        XCTAssertThrowsError(try f.restore(Data(repeating: 1, count: 64))) {
            XCTAssertEqual($0 as? DeviceKeyError, .keyUnavailable)
        }
    }
}

// The app target's sources, read from the checkout (the test runs on this Mac's simulator, which
// reads the host file system): the access-control call happens once, with the policy's flags, and
// no software P-256 key type appears outside the Secure Enclave's namespace. The software key
// the tests sign with is in TaptimeTests/Support.swift and nowhere else.
final class SourceGuardTests: XCTestCase {
    private func appSources() throws -> [(String, String)] {
        let dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().appendingPathComponent("Taptime")
        let names = try FileManager.default.contentsOfDirectory(atPath: dir.path).filter { $0.hasSuffix(".swift") }
        XCTAssertFalse(names.isEmpty)
        return try names.map { ($0, try String(contentsOf: dir.appendingPathComponent($0), encoding: .utf8)) }
    }

    private func count(_ pattern: String, in s: String) throws -> Int {
        try NSRegularExpression(pattern: pattern).numberOfMatches(in: s, range: NSRange(s.startIndex..., in: s))
    }

    func testOneAccessControlCallAndItTakesThePolicysFlags() throws {
        var calls = 0
        for (name, src) in try appSources() {
            let n = try count(#"SecAccessControlCreateWithFlags\("#, in: src)
            calls += n
            if n > 0 {
                XCTAssertEqual(name, "DeviceKey.swift")
                XCTAssertEqual(try count(#"SecAccessControlCreateWithFlags\(nil, protection, flags, &error\)"#, in: src), 1)
            }
        }
        XCTAssertEqual(calls, 1)
    }

    func testNoSoftwareKeyInTheApp() throws {
        for (name, src) in try appSources() {
            XCTAssertEqual(try count(#"(?<!SecureEnclave\.)P256\.Signing\.PrivateKey"#, in: src), 0, name)
            XCTAssertEqual(try count(#"kSecAttrKeyTypeEC|SecKeyCreateRandomKey"#, in: src), 0, name)
        }
    }
}

/// The activation record's Keychain item: written with the key's protection class, read back,
/// cleared. A service name of its own, so the hosted app's record is never touched.
final class KeychainActivationStoreTests: XCTestCase {
    private let store = KeychainActivationStore(service: "mt.taptime.app.device.unit", account: "activation")

    override func tearDown() {
        store.clear()
    }

    func testTheRecordRoundTripsAndClears() throws {
        store.clear()
        XCTAssertNil(store.load())
        let r = ActivationRecord(deviceId: "6f1c2a9e-3b4d-4e5f-8a7b-0c1d2e3f4a5b", employeeName: "Maria Borg",
                                 keyHandle: Data([1, 2, 3]))
        try store.save(r)
        XCTAssertEqual(store.load(), r)
        let r2 = ActivationRecord(deviceId: "7d1c2a4e-9b3f-4c5d-8e6f-0a1b2c3d4e5f", employeeName: "Maria Borg",
                                  keyHandle: Data([4]))
        try store.save(r2)
        XCTAssertEqual(store.load(), r2, "a new activation replaces the old record")

        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: "mt.taptime.app.device.unit",
            kSecReturnAttributes as String: true,
        ]
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        XCTAssertEqual(SecItemCopyMatching(q as CFDictionary, &out), errSecSuccess)
        let attrs = try XCTUnwrap(out as? [String: Any])
        XCTAssertEqual(attrs[kSecAttrAccessible as String] as? String, kSecAttrAccessibleWhenUnlockedThisDeviceOnly as String)

        store.clear()
        XCTAssertNil(store.load())
    }
}
