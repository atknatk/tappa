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

    func save(_ record: ActivationRecord) throws {
        guard let data = try? JSONEncoder().encode(record) else {
            throw ActivationStoreError.encoding
        }
        clear()
        var q = query
        q[kSecValueData as String] = data
        q[kSecAttrAccessible as String] = DeviceKeyPolicy.protection
        q[kSecAttrSynchronizable as String] = false
        let status = SecItemAdd(q as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw ActivationStoreError.keychain(status)
        }
    }

    func clear() {
        SecItemDelete(query as CFDictionary)
    }
}
