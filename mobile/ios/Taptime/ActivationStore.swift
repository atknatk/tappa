import Foundation
import Security

// WHAT THE APP REMEMBERS AFTER ACTIVATION: the device id the server issued, the name to greet,
// and the Secure Enclave key's wrapped handle. That is the whole of it -- no invite code, no
// challenge, no token (there is none to keep: ADR 0045 §2.3).
//
// It lives in ONE Keychain item with the key's own protection class: readable only while the
// phone is unlocked, never synchronised, never restored to another device. The handle is not a
// private key -- only this phone's Secure Enclave can use it -- but it is kept where a backup
// cannot carry it all the same, so a restored phone starts at activation instead of holding a
// handle that opens nothing.

struct ActivationRecord: Codable, Equatable {
    let deviceId: String
    let employeeName: String
    let keyHandle: Data
}

protocol ActivationStore {
    func load() -> ActivationRecord?
    func save(_ record: ActivationRecord) throws
    func clear()
}

enum ActivationStoreError: Error {
    case keychain(OSStatus)
    case encoding
}

struct KeychainActivationStore: ActivationStore {
    var service = "mt.taptime.app.device"
    var account = "activation"

    private var query: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    func load() -> ActivationRecord? {
        var q = query
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else {
            return nil
        }
        return try? JSONDecoder().decode(ActivationRecord.self, from: data)
    }

    /// Replaces the record IN PLACE (M16-16 hand-off 3): SecItemUpdate when an item exists, so there
    /// is no moment -- a crash, a failing add -- between a delete and an add where the phone holds
    /// no record at all while the server holds an active device. SecItemAdd only when there is none.
    /// The update rewrites the protection class too, so an item written before this rule keeps the
    /// key's class.
    func save(_ record: ActivationRecord) throws {
        guard let data = try? JSONEncoder().encode(record) else {
            throw ActivationStoreError.encoding
        }
        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: DeviceKeyPolicy.protection,
        ]
        var match = query
        match[kSecAttrSynchronizable as String] = false
        let updated = SecItemUpdate(match as CFDictionary, attributes as CFDictionary)
        switch updated {
        case errSecSuccess:
            return
        case errSecItemNotFound:
            var q = match
            q.merge(attributes) { _, new in new }
            let status = SecItemAdd(q as CFDictionary, nil)
            guard status == errSecSuccess else {
                throw ActivationStoreError.keychain(status)
            }
        default:
            throw ActivationStoreError.keychain(updated)
        }
    }

    func clear() {
        SecItemDelete(query as CFDictionary)
    }
}
