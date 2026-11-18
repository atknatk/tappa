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

        /**
         * The backing from what KeyInfo reported (device/KeystoreKeys.kt reads it). Pure, so every
         * combination is driven by a JVM test.
         *
         * API 31+ reports a security level; [level] is it in this app's words. A level this version
         * cannot name ([Level.OTHER], SECURITY_LEVEL_UNKNOWN included) is UNKNOWN -- accepted by
         * [KeyGate] -- ONLY when the older report, inside secure hardware, also says so; otherwise
         * it is SOFTWARE and refused (M16-12 hand-over (1): a cheap hardening, an unknown level is
         * no longer taken on trust). API 26-30 have only that older report: outside secure hardware
         * is SOFTWARE, a key generated with the StrongBox flag is STRONGBOX (the flag makes
         * generation fail otherwise), any other secure one SECURE_HARDWARE.
         */
        fun fromReport(sdk: Int, level: Level, insideSecureHardware: Boolean, inStrongBox: Boolean): KeyBacking {
            if (sdk >= 31) {
                return when (level) {
                    Level.STRONGBOX -> STRONGBOX
                    Level.TRUSTED_ENVIRONMENT -> TEE
                    Level.UNKNOWN_SECURE -> SECURE_HARDWARE
                    Level.SOFTWARE -> SOFTWARE
                    Level.OTHER -> if (insideSecureHardware) UNKNOWN else SOFTWARE
                }
            }
            return when {
                !insideSecureHardware -> SOFTWARE
                inStrongBox -> STRONGBOX
                else -> SECURE_HARDWARE
            }
        }
    }

    /** KeyInfo.getSecurityLevel's answers (API 31+), and OTHER for any value not named here. */
    enum class Level { STRONGBOX, TRUSTED_ENVIRONMENT, UNKNOWN_SECURE, SOFTWARE, OTHER }
}

/**
 * THE HARDWARE GATE (coordinator decision, 2026-11-18; the iOS twin refuses a phone without a
 * Secure Enclave). A key the Keystore reports as SOFTWARE is deleted before any bind and the
 * phone is told it cannot be set up: the device key is the app's whole proof of "who", and a
 * software key is a file. Every other level is accepted -- TEE, StrongBox, "secure hardware"
 * (API 26-30), and UNKNOWN (a level a newer platform reports that this version cannot name, while
 * the key reports itself inside secure hardware -- [KeyBacking.fromReport], M16-13). The level is
 * the generator's own report, handed in with the key, so the gate is a pure function the unit
 * tests drive with every value.
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
