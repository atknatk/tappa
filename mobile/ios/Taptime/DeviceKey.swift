import CryptoKit
import Foundation
import Security

// THE DEVICE KEY (ADR 0045 §2.3; M16-16). A P-256 signing key born in this iPhone's Secure
// Enclave: the private half never leaves the chip, the server stores only the public point, and
// every signed request is a fresh ECDSA signature over a server-chosen nonce or the server-time
// second -- there is no bearer token anywhere in the app.
//
// NO PERSON CHECK GUARDS THE KEY (CLAUDE.md §4.1). The access control below carries exactly ONE
// flag, privateKeyUsage, which is what lets the Secure Enclave sign with the key at all; it adds
// no prompt of any kind. The phone's own screen lock is not our layer: an unlocked phone can use
// the key, a locked one cannot (WhenUnlockedThisDeviceOnly), and nothing about the person is read.
// DeviceKeyPolicyTests holds the flags to that one value as an allow-list.
//
// NO SOFTWARE FALLBACK. If this iPhone has no Secure Enclave (every NFC iPhone has one; a
// simulator may or may not -- the iOS 26.3 simulator on the build Mac reports one, measured
// 2026-11-18), generate() throws .secureEnclaveUnavailable and the app says it cannot be set up on
// this phone. There is no path in this target that makes a key anywhere else: the
// software key the unit tests sign with lives in the TEST target only (TaptimeTests).

/// What signs for this device. The production conformer is SecureEnclaveDeviceKey.
protocol DeviceSigner {
    /// The raw uncompressed point, 04 || X || Y, 65 bytes -- the wire's publicKey before base64url.
    var publicPoint: Data { get }
    /// What the activation record keeps so the key can be found again. For the Secure Enclave key
    /// this is the chip's wrapped handle, usable only by this phone's Secure Enclave.
    var persistentHandle: Data { get }
    /// An ECDSA P-256 / SHA-256 signature over message (the signer hashes it), ASN.1 DER.
    func sign(_ message: Data) throws -> Data
}

/// Makes and restores device keys. The app holds the Secure Enclave factory; tests inject theirs.
protocol DeviceKeyFactory {
    /// Whether keys can be made here at all. The app answers SecureEnclave.isAvailable; false
    /// means the app cannot be set up on this phone (never a software key).
    var isAvailable: Bool { get }
    func newKey() throws -> DeviceSigner
    func restore(_ handle: Data) throws -> DeviceSigner
}

enum DeviceKeyError: Error, Equatable {
    /// No Secure Enclave on this device: activation cannot happen here (no software fallback).
    case secureEnclaveUnavailable
    /// The access control or the key could not be created or restored.
    case keyUnavailable
}

/// The key's whole access policy, in one place, so the test reads exactly what the factory uses.
enum DeviceKeyPolicy {
    /// Usable only while the phone is unlocked, and never copied to a backup or another device.
    static let protection: CFString = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
    /// privateKeyUsage only: the Secure Enclave may sign with the key. No other flag.
    static let flags: SecAccessControlCreateFlags = [.privateKeyUsage]

    static func accessControl() throws -> SecAccessControl {
        var error: Unmanaged<CFError>?
        guard let ac = SecAccessControlCreateWithFlags(nil, protection, flags, &error) else {
            error?.release()
            throw DeviceKeyError.keyUnavailable
        }
        return ac
    }
}

struct SecureEnclaveDeviceKey: DeviceSigner {
    private let key: SecureEnclave.P256.Signing.PrivateKey

    var publicPoint: Data { key.publicKey.x963Representation }
    var persistentHandle: Data { key.dataRepresentation }

    func sign(_ message: Data) throws -> Data {
        try key.signature(for: message).derRepresentation
    }

    /// A NEW key on every call: the app generates a fresh pair for every activation (the M16-03
    /// review: a key the server already knows answers new_key_required, never reuse).
    static func generate() throws -> SecureEnclaveDeviceKey {
        guard SecureEnclave.isAvailable else {
            throw DeviceKeyError.secureEnclaveUnavailable
        }
        let ac = try DeviceKeyPolicy.accessControl()
        do {
            return SecureEnclaveDeviceKey(key: try SecureEnclave.P256.Signing.PrivateKey(
                compactRepresentable: false, accessControl: ac))
        } catch {
            throw DeviceKeyError.keyUnavailable
        }
    }

    static func restore(_ handle: Data) throws -> SecureEnclaveDeviceKey {
        guard SecureEnclave.isAvailable else {
            throw DeviceKeyError.secureEnclaveUnavailable
        }
        do {
            return SecureEnclaveDeviceKey(key: try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: handle))
        } catch {
            throw DeviceKeyError.keyUnavailable
        }
    }
}

struct SecureEnclaveKeyFactory: DeviceKeyFactory {
    var isAvailable: Bool { SecureEnclave.isAvailable }
    func newKey() throws -> DeviceSigner { try SecureEnclaveDeviceKey.generate() }
    func restore(_ handle: Data) throws -> DeviceSigner { try SecureEnclaveDeviceKey.restore(handle) }
}

enum DevicePublicKey {
    static let pointLength = 65

    /// Whether data is a key the server would store: exactly the 65-byte uncompressed form, and a
    /// point on P-256 (CryptoKit's parser refuses off-curve and out-of-range coordinates). The app
    /// only ever sends its own keys; this is the check before it does.
    static func isValidPoint(_ data: Data) -> Bool {
        guard data.count == pointLength, data.first == 0x04 else {
            return false
        }
        return (try? P256.Signing.PublicKey(x963Representation: data)) != nil
    }
}
