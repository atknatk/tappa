package mt.taptime.app

import mt.taptime.app.wire.B64Url
import mt.taptime.app.wire.Canonical
import mt.taptime.app.wire.PublicPoint
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

/**
 * M16-05's and M16-07's known-answer vectors (test/fixtures/appdevice_vectors.json), read by
 * the Kotlin side as the file asks: "the iOS and Android request builders are to read it too,
 * so the three implementations are held to ONE byte string." Every canonical text was written
 * with printf and every signature made by OpenSSL, not by Go -- so this checks the app's builder
 * against a third implementation, not against the server's code.
 *
 * The file is a declared input of the test task (build.gradle.kts, serverFiles): a vector added
 * or changed on the server side re-runs this class.
 */
class VectorsTest {
    @Suppress("UNCHECKED_CAST")
    private val v: Map<String, Any?> by lazy { MiniJson.decodeObject(serverFile("taptime.vectors").readText()) }

    @Suppress("UNCHECKED_CAST")
    private fun list(k: String) = v[k] as List<Map<String, Any?>>

    @Suppress("UNCHECKED_CAST")
    private fun point(key: String): ByteArray =
        hexBytes(((v["keys"] as Map<String, Any?>)[key] as Map<String, Any?>)["public_point_hex"] as String)

    private fun body(m: Map<String, Any?>) = (m["body_utf8"] as String).toByteArray(Charsets.UTF_8)

    private fun requestText(name: String): ByteArray {
        val r = list("requests").single { it["name"] == name }
        return Canonical.request(r["method"] as String, r["path"] as String, body(r), r["nonce"] as String, r["device_id"] as String)
    }

    private fun readText(name: String): ByteArray {
        val r = list("reads").single { it["name"] == name }
        return Canonical.read(r["method"] as String, r["path"] as String, body(r), r["timestamp"] as String, r["device_id"] as String)
    }

    @Test
    fun theControlsSeeTheWholeFile() {
        // A file that silently lost its lists would make every loop below vacuous.
        assertEquals(4, list("requests").size)
        assertEquals(8, list("signatures").size)
        assertEquals(12, list("malformed_requests").size)
        assertEquals(3, list("reads").size)
        assertEquals(6, list("read_signatures").size)
        assertEquals(7, list("invalid_public_keys").size)
    }

    @Test
    fun everyRequestsCanonicalTextIsByteForByteTheVectors() {
        for (r in list("requests")) {
            val name = r["name"] as String
            val text = requestText(name)
            assertEquals("$name: body digest", r["body_sha256_hex"], Canonical.hex(Canonical.sha256(body(r))))
            assertArrayEquals("$name: canonical text", (r["canonical_text"] as String).toByteArray(Charsets.US_ASCII), text)
            assertEquals("$name: canonical digest", r["canonical_sha256_hex"], Canonical.hex(Canonical.sha256(text)))
        }
    }

    @Test
    fun everyReadsCanonicalTextIsByteForByteTheVectors() {
        for (r in list("reads")) {
            val name = r["name"] as String
            val text = readText(name)
            assertEquals("$name: body digest", r["body_sha256_hex"], Canonical.hex(Canonical.sha256(body(r))))
            assertArrayEquals("$name: canonical text", (r["canonical_text"] as String).toByteArray(Charsets.US_ASCII), text)
            assertEquals("$name: canonical digest", r["canonical_sha256_hex"], Canonical.hex(Canonical.sha256(text)))
        }
    }

    @Test
    fun everyRequestSignatureVerifiesExactlyWhenTheVectorSaysValid() {
        var valid = 0
        for (s in list("signatures")) {
            val name = s["name"] as String
            val sig = B64Url.decode(s["signature_b64url"] as String) ?: throw AssertionError("$name: not unpadded base64url")
            val got = verifies(point(s["key"] as String), requestText(s["request"] as String), sig)
            assertEquals("$name: verifies", s["valid"] as Boolean, got)
            if (got) valid++
        }
        assertEquals("CONTROL: five of the eight request signatures are valid", 5, valid)
    }

    @Test
    fun everyReadSignatureVerifiesExactlyWhenTheVectorSaysValid() {
        var valid = 0
        for (s in list("read_signatures")) {
            val name = s["name"] as String
            val sig = B64Url.decode(s["signature_b64url"] as String) ?: throw AssertionError("$name: not unpadded base64url")
            val got = verifies(point(s["key"] as String), readText(s["read"] as String), sig)
            assertEquals("$name: verifies", s["valid"] as Boolean, got)
            if (got) valid++
        }
        assertEquals("CONTROL: three of the six read signatures are valid", 3, valid)
    }

    @Test
    fun everyMalformedRequestIsRefusedByTheBuilder() {
        for (r in list("malformed_requests")) {
            val name = r["name"] as String
            try {
                Canonical.request(r["method"] as String, r["path"] as String, ByteArray(0), r["nonce"] as String, r["device_id"] as String)
                fail("$name: the builder signed a text the server refuses")
            } catch (_: Canonical.Malformed) {
                // the server's ErrMalformed
            }
        }
    }

    @Test
    fun theSpkiVectorIsKeyAOnceTheHeaderIsCut() {
        val spki = hexBytes(list("invalid_public_keys").single { it["name"] == "spki" }["public_point_hex"] as String)
        assertArrayEquals(point("A"), PublicPoint.fromSpki(spki))
        assertArrayEquals(spki, PublicPoint.toSpki(point("A")))
        // The bare point, a truncated wrapper and a wrapper around a compressed point are not SPKI of P-256.
        assertNull(PublicPoint.fromSpki(point("A")))
        assertNull(PublicPoint.fromSpki(spki.copyOf(spki.size - 1)))
        val compressed = spki.copyOf().also { it[26] = 0x03 }
        assertNull(PublicPoint.fromSpki(compressed))
    }

    @Test
    fun theKeysOnTheWireAre65BytePointsIn87Characters() {
        for (k in listOf("A", "B", "C")) {
            val p = point(k)
            assertEquals(PublicPoint.LEN, p.size)
            val wire = B64Url.encode(p)
            assertEquals(87, wire.length)
            assertFalse("unpadded", wire.contains('='))
            assertArrayEquals(p, B64Url.decode(wire))
        }
    }

    @Test
    fun aKeyThisSideMakesSignsWhatTheVectorsFormatVerifies() {
        // The signer path end to end, with a JDK key standing in for the Keystore's: a public
        // point cut out of the SPKI, a DER signature over a canonical text built here, verified
        // under the raw point the way the server will.
        val key = JdkSigner()
        val r = list("requests").single { it["name"] == "tap" }
        val text = requestText("tap")
        val sig = key.sign(text)
        assertTrue(verifies(key.publicPoint, text, sig))
        assertFalse("a signature does not carry over to another request's text", verifies(key.publicPoint, requestText("revoke-empty-body"), sig))
        assertNotNull(r)
    }
}
