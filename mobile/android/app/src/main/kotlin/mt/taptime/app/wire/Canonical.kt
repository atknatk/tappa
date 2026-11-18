package mt.taptime.app.wire

import java.security.MessageDigest
import java.util.Base64
import java.util.UUID

// THE SIGNED BYTES (ADR 0045 §2.3; internal/appdevice/canonical.go and read.go are the
// definition, test/fixtures/appdevice_vectors.json the known answers this file is held to).
// Six lines, each ended by one LF, ASCII only:
//
//	prefix          tappa-app-request-v1 (nonce-signed) or tappa-app-read-v1 (signed read)
//	method          upper case
//	path            the path as sent, no query
//	body digest     lower-case hex SHA-256 of the RAW body bytes sent
//	freshness       the nonce as issued (43 base64url) or the decimal Unix second
//	device id       canonical lower-case UUID
//
// Every field is validated to the server's closed character set BEFORE it is written, so a
// value the server would refuse is refused here too: the app never signs a text the server
// cannot rebuild. The refusals carry no value (§7) -- the caller knows what it passed.
object Canonical {
    const val REQUEST_PREFIX = "tappa-app-request-v1"
    const val READ_PREFIX = "tappa-app-read-v1"

    private const val MAX_METHOD = 16
    private const val MAX_PATH = 1024
    private const val NONCE_LEN = 43
    private const val NONCE_BYTES = 32
    private const val MAX_TIMESTAMP_DIGITS = 19

    class Malformed(what: String) : IllegalArgumentException("canonical text: malformed $what")

    /** The nonce-signed text: POST /app/v1/tap, POST /app/v1/revoke, POST /app/v1/activate/complete. */
    fun request(method: String, path: String, body: ByteArray, nonce: String, deviceId: String): ByteArray {
        checkMethodAndPath(method, path)
        if (!validNonce(nonce)) throw Malformed("nonce")
        if (!validDeviceId(deviceId)) throw Malformed("device id")
        return build(REQUEST_PREFIX, method, path, body, nonce, deviceId)
    }

    /** The signed read's text: GET /app/v1/me, POST /app/v1/tap/start, POST /app/v1/nonce. */
    fun read(method: String, path: String, body: ByteArray, timestamp: String, deviceId: String): ByteArray {
        checkMethodAndPath(method, path)
        if (!validTimestamp(timestamp)) throw Malformed("timestamp")
        if (!validDeviceId(deviceId)) throw Malformed("device id")
        return build(READ_PREFIX, method, path, body, timestamp, deviceId)
    }

    fun sha256(b: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(b)

    fun hex(b: ByteArray): String {
        val out = StringBuilder(b.size * 2)
        for (x in b) {
            val v = x.toInt() and 0xff
            out.append(HEX[v ushr 4]).append(HEX[v and 0x0f])
        }
        return out.toString()
    }

    private const val HEX = "0123456789abcdef"

    private fun build(prefix: String, method: String, path: String, body: ByteArray, freshness: String, deviceId: String): ByteArray {
        val text = StringBuilder()
        for (field in listOf(prefix, method, path, hex(sha256(body)), freshness, deviceId)) {
            text.append(field).append('\n')
        }
        return text.toString().toByteArray(Charsets.US_ASCII)
    }

    private fun checkMethodAndPath(method: String, path: String) {
        if (method.isEmpty() || method.length > MAX_METHOD || method.any { it !in 'A'..'Z' }) throw Malformed("method")
        // Visible ASCII only, starting with '/', no query and no fragment: no LF can enter the
        // text through the path, and an unsigned query is refused by the server anyway.
        if (path.isEmpty() || path.length > MAX_PATH || path[0] != '/' ||
            path.any { it.code < 0x21 || it.code > 0x7e || it == '?' || it == '#' }
        ) throw Malformed("path")
    }

    /** The server's own round trip: only the string it would have written (no padding, URL alphabet, zero spare bits). */
    fun validNonce(s: String): Boolean {
        if (s.length != NONCE_LEN) return false
        val raw = try {
            Base64.getUrlDecoder().decode(s)
        } catch (_: IllegalArgumentException) {
            return false
        }
        return raw.size == NONCE_BYTES && Base64.getUrlEncoder().withoutPadding().encodeToString(raw) == s
    }

    /** Canonical lower-case hyphenated form, not the nil UUID: what Go's uuid.UUID.String() prints. */
    fun validDeviceId(s: String): Boolean {
        if (s.length != 36) return false
        val id = try {
            UUID.fromString(s)
        } catch (_: IllegalArgumentException) {
            return false
        }
        return id.toString() == s && id != UUID(0L, 0L)
    }

    /** Decimal, no sign, no leading zero (the server's parseTimestamp). */
    fun validTimestamp(s: String): Boolean =
        s.isNotEmpty() && s.length <= MAX_TIMESTAMP_DIGITS && s.all { it in '0'..'9' } && (s == "0" || s[0] != '0') &&
            s.toBigInteger() <= Long.MAX_VALUE.toBigInteger()
}

/** Unpadded base64url, the encoding of App-Signature and of the body's publicKey. */
object B64Url {
    fun encode(b: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(b)

    /** Strict: refuses padding, the standard alphabet and any string that does not round-trip. */
    fun decode(s: String): ByteArray? {
        val raw = try {
            Base64.getUrlDecoder().decode(s)
        } catch (_: IllegalArgumentException) {
            return null
        }
        return if (encode(raw) == s) raw else null
    }
}
