package mt.taptime.app.activation

import mt.taptime.app.wire.Signer

/**
 * Where the private half lives, as Android's Keystore reports it after generation (KeyInfo):
 * checked by [KeyGate] before the key is bound, and recorded with the device (DeviceState) so a
 * later card -- attestation (M16-25), the device tour (M16-14) -- can read what this phone
 * actually gave us. It is not sent: the wire contract has no field for it, and the server
 * decodes strictly.
 */
enum class KeyBacking(val stored: String) {
    /** A separate secure element (API 28+, FEATURE_STRONGBOX_KEYSTORE). */
    STRONGBOX("strongbox"),
    /** The trusted execution environment: the ordinary hardware-backed Keystore. */
    TEE("tee"),
    /** Hardware-backed, level not reported (API 26-30 report only "inside secure hardware"). */
    SECURE_HARDWARE("secure_hardware"),
    /**
     * Keystore in software (emulators, some old phones). REFUSED for activation ([KeyGate]): the
     * private half then lives in ordinary memory and storage, where root can read it.
     */
    SOFTWARE("software"),
    /** The platform answered something this app does not know. */
    UNKNOWN("unknown"),
    ;

    companion object {
        fun of(stored: String?): KeyBacking = entries.firstOrNull { it.stored == stored } ?: UNKNOWN
    }
}

/**
 * THE HARDWARE GATE (coordinator decision, 2026-11-18; the iOS twin refuses a phone without a
 * Secure Enclave). A key the Keystore reports as SOFTWARE is deleted before any bind and the
 * phone is told it cannot be set up: the device key is the app's whole proof of "who", and a
 * software key is a file. Every other level is accepted -- TEE, StrongBox, "secure hardware"
 * (API 26-30), and UNKNOWN (a level a newer platform reports that this version cannot name is not
 * evidence of software). The level is the generator's own report, handed in with the key, so the
 * gate is a pure function the unit tests drive with every value.
 */
object KeyGate {
    fun accepts(backing: KeyBacking): Boolean = backing != KeyBacking.SOFTWARE
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
