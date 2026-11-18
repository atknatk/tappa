package mt.taptime.app

import java.io.File
import java.security.KeyFactory
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.security.spec.X509EncodedKeySpec
import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.DeviceKey
import mt.taptime.app.activation.DeviceKeys
import mt.taptime.app.activation.DeviceState
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.wire.PublicPoint
import mt.taptime.app.wire.Signer
import org.junit.Assert.assertTrue

/** A repository file handed over by the Gradle test task (build.gradle.kts, serverFiles). */
fun serverFile(property: String): File {
    val path = System.getProperty(property)
        ?: throw AssertionError("system property $property is not set -- run through Gradle (make mobile-android-test)")
    val f = File(path)
    assertTrue("$property points at a missing file: $path", f.isFile)
    return f
}

fun hexBytes(s: String): ByteArray {
    require(s.length % 2 == 0) { "odd hex length" }
    return ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }
}

/** Verifies a DER ECDSA P-256 / SHA-256 signature with the JDK, under a RAW 65-byte point. */
fun verifies(point: ByteArray, message: ByteArray, der: ByteArray): Boolean {
    val pub = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(PublicPoint.toSpki(point)))
    return Signature.getInstance("SHA256withECDSA").run {
        initVerify(pub)
        update(message)
        try {
            verify(der)
        } catch (_: java.security.SignatureException) {
            false
        }
    }
}

/** A software P-256 key from the JDK: the unit tests' stand-in for the Keystore key. */
class JdkSigner(private val pair: KeyPair = newPair()) : Signer {
    override val publicPoint: ByteArray = PublicPoint.fromSpki(pair.public.encoded)
        ?: throw AssertionError("the JDK's P-256 SPKI did not yield a point")

    var signatures = 0
        private set

    override fun sign(message: ByteArray): ByteArray {
        signatures++
        return Signature.getInstance("SHA256withECDSA").run {
            initSign(pair.private)
            update(message)
            sign()
        }
    }

    companion object {
        fun newPair(): KeyPair = KeyPairGenerator.getInstance("EC").run {
            initialize(ECGenParameterSpec("secp256r1"))
            generateKeyPair()
        }
    }
}

/** DeviceKeys over JDK keys, recording what was generated and deleted. */
class JdkKeys : DeviceKeys {
    val live = LinkedHashMap<String, JdkSigner>()
    val deleted = mutableListOf<String>()
    private var n = 0

    override fun generate(): DeviceKey {
        val alias = "test-key-${++n}"
        val s = JdkSigner()
        live[alias] = s
        return DeviceKey(alias, KeyBacking.SOFTWARE, s)
    }

    override fun load(alias: String): Signer? = live[alias]

    override fun delete(alias: String) {
        if (live.remove(alias) != null) deleted += alias
    }
}

class MemoryState(var device: ActiveDevice? = null) : DeviceState {
    override fun active(): ActiveDevice? = device
    override fun save(device: ActiveDevice) {
        this.device = device
    }
    override fun clear() {
        device = null
    }
}
