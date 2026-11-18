package mt.taptime.app.activation

import mt.taptime.app.wire.Signer

/**
 * Where the private half lives, as Android's Keystore reports it after generation (KeyInfo):
 * recorded with the device (DeviceState) so a later card -- attestation (M16-25), the device
 * tour (M16-14) -- can read what this phone actually gave us. It is not sent: the wire contract
 * has no field for it, and the server decodes strictly.
 */
enum class KeyBacking(val stored: String) {
    /** A separate secure element (API 28+, FEATURE_STRONGBOX_KEYSTORE). */
    STRONGBOX("strongbox"),
    /** The trusted execution environment: the ordinary hardware-backed Keystore. */
    TEE("tee"),
    /** Hardware-backed, level not reported (API 26-30 report only "inside secure hardware"). */
    SECURE_HARDWARE("secure_hardware"),
    /** Keystore in software (emulators, some old phones): still not exportable through the API. */
    SOFTWARE("software"),
    /** The platform answered something this app does not know. */
    UNKNOWN("unknown"),
    ;

    companion object {
        fun of(stored: String?): KeyBacking = entries.firstOrNull { it.stored == stored } ?: UNKNOWN
    }
}

/** A key just generated: its Keystore alias, where it lives, and the signer over it. */
class DeviceKey(val alias: String, val backing: KeyBacking, val signer: Signer)

/** The key store, as the activation needs it. Production: KeystoreKeys; tests: a JDK key. */
interface DeviceKeys {
    /** A NEW key pair under a new alias (M16-07: every activation binds a fresh pair). */
    fun generate(): DeviceKey

    /** The signer of an existing alias, or null when the entry is gone. */
    fun load(alias: String): Signer?

    fun delete(alias: String)
}

/** The device this app is, once activated. No secret: the id is public, the key stays in the Keystore. */
data class ActiveDevice(
    val deviceId: String,
    val keyAlias: String,
    val backing: KeyBacking,
    val employeeName: String,
)

interface DeviceState {
    fun active(): ActiveDevice?
    fun save(device: ActiveDevice)
    fun clear()
}
