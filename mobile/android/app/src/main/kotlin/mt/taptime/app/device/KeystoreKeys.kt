package mt.taptime.app.device

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.ProviderException
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.util.UUID
import mt.taptime.app.activation.DeviceKey
import mt.taptime.app.activation.DeviceKeys
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.wire.PublicPoint
import mt.taptime.app.wire.Signer

// THE DEVICE KEY (ADR 0045 §2.3, card M16-12): a P-256 signing key generated INSIDE Android's
// Keystore. Its private half is never exportable -- the Keystore hands out a handle, and
// PrivateKey.getEncoded() of a Keystore key is null -- so the app cannot copy it and neither can
// a backup (the manifest turns both backup paths off besides, M16-11).
//
// WHERE IT LIVES. StrongBox when the phone has one (API 28+, FEATURE_STRONGBOX_KEYSTORE),
// otherwise the TEE. A StrongBox generation that fails (the feature is declared but the element
// is busy or refuses P-256 -- StrongBoxUnavailableException, a ProviderException) falls back to
// the TEE once. Where the key really ended up is READ BACK from KeyInfo, not assumed from what
// was asked: API 31+ reports the security level itself; on 26-30 only "inside secure hardware"
// is reported, and a key generated with the StrongBox flag set is a StrongBox key (the flag makes
// generation fail otherwise). The result is recorded with the device (DeviceState).
//
// NO USER-AUTHENTICATION REQUIREMENT IS SET on this key (CLAUDE.md §4.1, ADR 0045 §2.3): the
// phone's own screen lock is not Taptime's layer, and an unlocked phone may sign. The red-line
// scan's R1 fails on the Keystore builder method that would add one anywhere under mobile/, so
// this sentence is enforced, not only written.
class KeystoreKeys(private val context: Context) : DeviceKeys {

    override fun generate(): DeviceKey {
        val alias = ALIAS_PREFIX + UUID.randomUUID()
        val strongBoxAsked = Build.VERSION.SDK_INT >= 28 &&
            context.packageManager.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
        val inStrongBox = if (strongBoxAsked) {
            try {
                generatePair(alias, strongBox = true)
                true
            } catch (_: ProviderException) {
                delete(alias)
                generatePair(alias, strongBox = false)
                false
            }
        } else {
            generatePair(alias, strongBox = false)
            false
        }
        // A key whose level cannot be read is deleted before the failure goes up (M16-12 hand-over
        // (4)): it was never bound, nothing refers to its alias, and it would sit in the Keystore
        // for good.
        val backing = try {
            backingOf(alias, inStrongBox)
        } catch (e: Exception) {
            try {
                delete(alias)
            } catch (_: Exception) {
            }
            throw e
        }
        return DeviceKey(alias, backing, KeystoreSigner(alias))
    }

    override fun load(alias: String): Signer? =
        if (keyStore().containsAlias(alias)) KeystoreSigner(alias) else null

    override fun delete(alias: String) {
        val ks = keyStore()
        if (ks.containsAlias(alias)) ks.deleteEntry(alias)
    }

    private fun generatePair(alias: String, strongBox: Boolean) {
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
        if (strongBox && Build.VERSION.SDK_INT >= 28) spec.setIsStrongBoxBacked(true)
        KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, KEYSTORE).run {
            initialize(spec.build())
            generateKeyPair()
        }
    }

    @Suppress("DEPRECATION") // isInsideSecureHardware: the only report API 26-30 have, and the cross-check of 31+'s unknown
    private fun backingOf(alias: String, inStrongBox: Boolean): KeyBacking {
        val key = keyStore().getKey(alias, null) as PrivateKey
        val info = KeyFactory.getInstance(key.algorithm, KEYSTORE).getKeySpec(key, KeyInfo::class.java)
        val level = if (Build.VERSION.SDK_INT >= 31) {
            when (info.securityLevel) {
                KeyProperties.SECURITY_LEVEL_STRONGBOX -> KeyBacking.Level.STRONGBOX
                KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> KeyBacking.Level.TRUSTED_ENVIRONMENT
                KeyProperties.SECURITY_LEVEL_UNKNOWN_SECURE -> KeyBacking.Level.UNKNOWN_SECURE
                KeyProperties.SECURITY_LEVEL_SOFTWARE -> KeyBacking.Level.SOFTWARE
                else -> KeyBacking.Level.OTHER
            }
        } else {
            KeyBacking.Level.OTHER
        }
        return KeyBacking.fromReport(Build.VERSION.SDK_INT, level, info.isInsideSecureHardware, inStrongBox)
    }

    private inner class KeystoreSigner(private val alias: String) : Signer {
        override val publicPoint: ByteArray by lazy {
            val spki = keyStore().getCertificate(alias)?.publicKey?.encoded
                ?: throw IllegalStateException("device key entry has no public half")
            PublicPoint.fromSpki(spki) ?: throw IllegalStateException("device key is not a P-256 point")
        }

        override fun sign(message: ByteArray): ByteArray {
            val key = keyStore().getKey(alias, null) as? PrivateKey
                ?: throw IllegalStateException("device key entry is gone")
            return Signature.getInstance("SHA256withECDSA").run {
                initSign(key)
                update(message)
                sign()
            }
        }
    }

    private fun keyStore(): KeyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val ALIAS_PREFIX = "taptime-device-"
    }
}
