package mt.taptime.app

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import java.net.InetSocketAddress
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import java.util.UUID
import kotlin.math.abs

/**
 * An in-process stand-in for /app/v1's activation routes and GET /me, speaking M16-07's wire
 * contract, so the flow tests drive the REAL HttpUrlTransport (java.net.HttpURLConnection) and
 * AppClient over a socket. com.sun.net.httpserver is the JDK's (module jdk.httpserver, on the
 * unit-test JVM by default -- measured; the relay's FakePanel uses it the same way).
 *
 * It verifies signatures the way the server does, but builds the canonical text ITSELF, by
 * string concatenation from the format's description (canonical.go's header), not with the app's
 * Canonical -- otherwise a wrong builder would agree with itself. The JDK writes the Date header
 * on every answer (measured: 200, 204, 401, 4xx), as Go's net/http does.
 *
 * Its rules are the parts of CompleteByTap's order the app can observe: an unknown code is
 * invalid_invite; consent is bound to the last bind's key and challenge (a later bind moves it);
 * a SUN query without ctr is activation_needs_touch; a ctr that does not advance is
 * activation_tap_failed (the atomic counter, §4.4); [newKeyOnce] answers new_key_required once.
 */
class FakeAppServer(
    val code: String,
    private val secondDevice: Boolean = false,
    private val accent: String? = null,
) : AutoCloseable {
    class Bound(val activationId: String, val challenge: String, val point: ByteArray)

    val seen = mutableListOf<String>()
    var bound: Bound? = null
        private set
    var lastCtr = -1
    var newKeyOnce = false
    /** When set, the next complete answers this status and error code, once. */
    var completeFailsOnce: Pair<Int, String>? = null
    var completed = 0
        private set
    /** Device id -> public point; removing an entry is the panel's "sign out their phones". */
    val devices = LinkedHashMap<String, ByteArray>()
    /** When set, every route answers 307 to this location (a captive portal, a hijacked proxy). */
    var redirectTo: String? = null
    /** The server's clock, Unix seconds; a test may skew it. */
    var nowSeconds: () -> Long = { System.currentTimeMillis() / 1000 }

    private val random = SecureRandom()
    private val server: HttpServer = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0).apply {
        createContext("/") { ex -> ex.use { handle(it) } }
        start()
    }

    val base: String = "http://127.0.0.1:${server.address.port}"

    override fun close() = server.stop(0)

    /** Another client's bind (the browser's consent POST, or a second phone): the binding moves. */
    fun bindElsewhere() {
        bound = Bound(UUID.randomUUID().toString(), b64(ByteArray(32).also(random::nextBytes)), ByteArray(65).also { it[0] = 4 })
    }

    private fun handle(ex: HttpExchange) {
        val path = ex.requestURI.rawPath
        seen += "${ex.requestMethod} $path"
        val body = ex.requestBody.readBytes()
        redirectTo?.let {
            ex.responseHeaders.set("Location", it)
            ex.sendResponseHeaders(307, -1)
            return
        }
        when ("${ex.requestMethod} $path") {
            "POST /app/v1/activate/lookup" -> lookup(ex, body)
            "POST /app/v1/activate/bind" -> bind(ex, body)
            "POST /app/v1/activate/complete" -> complete(ex, body)
            "POST /app/v1/activate/approval" -> approval(ex, body)
            "POST /app/v1/activate/approval/status" -> approvalStatus(ex, body)
            "GET /app/v1/me" -> me(ex, body)
            "POST /app/v1/nonce" -> nonce(ex, body)
            "POST /app/v1/tap/start" -> tapStart(ex, body)
            "POST /app/v1/tap" -> tap(ex, body)
            "POST /app/v1/hours" -> hours(ex, body)
            "POST /app/v1/revoke", "POST /app/v1/account/delete" -> forget(ex, body, path)
            else -> error(ex, 404, "not_found")
        }
    }

    private fun lookup(ex: HttpExchange, body: ByteArray) {
        val m = MiniJson.decodeObject(String(body))
        if (m.keys != setOf("code")) return error(ex, 400, "bad_request")
        if (m["code"] != code) return error(ex, 400, "invalid_invite")
        val brand = if (accent != null) """{"accent":"$accent"}""" else "{}"
        val approval = managerApproval?.let { ""","managerApproval":$it""" } ?: ""
        json(ex, """{"employeeName":"Maria Borg","businessName":"KF St Julians","locationName":"St Julians","wifiSsid":"KF-Staff","secondDevice":$secondDevice,"brand":$brand$approval}""")
    }

    private fun bind(ex: HttpExchange, body: ByteArray) {
        val m = MiniJson.decodeObject(String(body))
        if (m.keys != setOf("code", "consent", "publicKey")) return error(ex, 400, "bad_request")
        if (m["code"] != code) return error(ex, 400, "invalid_invite")
        if (m["consent"] != true) return error(ex, 400, "consent_required")
        val point = unb64(m["publicKey"] as String)
        if (point == null || point.size != 65 || point[0] != 4.toByte()) return error(ex, 400, "invalid_key")
        val b = Bound(UUID.randomUUID().toString(), b64(ByteArray(32).also(random::nextBytes)), point)
        bound = b
        json(ex, """{"activationId":"${b.activationId}","challenge":"${b.challenge}"}""")
    }

    private fun complete(ex: HttpExchange, body: ByteArray) {
        completeFailsOnce?.let { (status, code) ->
            completeFailsOnce = null
            return error(ex, status, code)
        }
        val id = ex.requestHeaders.getFirst("App-Device-Id")
        val nonce = ex.requestHeaders.getFirst("App-Nonce")
        val sig = ex.requestHeaders.getFirst("App-Signature")?.let(::unb64)
        if (id == null || nonce == null || sig == null || ex.requestHeaders.getFirst("App-Timestamp") != null) return unauthenticated(ex)
        val m = MiniJson.decodeObject(String(body))
        completeBodies += String(body)
        // EXACTLY ONE of the plaque's `sun` and the screen's `liveQR` (M16-23); both is 400.
        val base = setOf("code", "publicKey", "platform")
        if ((m.keys != base + "sun" && m.keys != base + "liveQR") || m["platform"] != "android") return error(ex, 400, "bad_request")
        val point = unb64(m["publicKey"] as String) ?: return error(ex, 400, "bad_request")
        // Proof of possession: the key the body names signed THIS request over the challenge.
        if (!verifies(point, text("tappa-app-request-v1", "POST", "/app/v1/activate/complete", body, nonce, id), sig)) {
            return unauthenticated(ex)
        }
        if (m["code"] != code) return error(ex, 400, "invalid_invite")
        if (m.containsKey("liveQR")) return completeLive(ex, id, nonce, point, m["liveQR"] as String)
        val sun = m["sun"] as String
        val params = sun.split('&').associate { it.substringBefore('=') to it.substringAfter('=', "") }
        val ctr = params["ctr"]?.toIntOrNull(16) ?: return error(ex, 400, "activation_needs_touch")
        val b = bound
        if (b == null || b.activationId != id || b.challenge != nonce || !b.point.contentEquals(point)) {
            return error(ex, 409, "activation_not_ready")
        }
        if (ctr <= lastCtr) return error(ex, 400, "activation_tap_failed")
        lastCtr = ctr
        if (newKeyOnce) {
            newKeyOnce = false
            return error(ex, 409, "new_key_required")
        }
        val deviceId = UUID.randomUUID().toString()
        devices.clear() // Q40 (a): the person's other app devices are revoked in the same transaction
        devices[deviceId] = point
        bound = null
        completed++
        json(ex, """{"deviceId":"$deviceId","employeeName":"Maria Borg"}""")
    }

    // --- M16-23: the phone that cannot tap ------------------------------------------------------

    /** What lookup says about the approval path; null leaves the field out (an older server). */
    var managerApproval: Boolean? = null
    /** Every complete body, raw, in order. */
    val completeBodies = mutableListOf<String>()
    /** The live QR queries a complete accepted, and the one it refuses (every refusal one code). */
    val liveCompleted = mutableListOf<String>()
    var liveRefused: String? = null
    /** When set, the next approval request answers this status and error code, once. */
    var approvalFailsOnce: Pair<Int, String>? = null
    /** approval/status answers, consumed in order: "waiting", "approved", "rejected", "expired", or "409:new_key_required". */
    val statusScript = ArrayDeque<String>()
    val approvalBodies = mutableListOf<String>()
    var approvals = 0
        private set
    var polls = 0
        private set
    val pairingCode = "482913"

    private fun completeLive(ex: HttpExchange, id: String, nonce: String, point: ByteArray, live: String) {
        val b = bound
        if (b == null || b.activationId != id || b.challenge != nonce || !b.point.contentEquals(point)) {
            return error(ex, 409, "activation_not_ready")
        }
        if (live == liveRefused) return error(ex, 400, "activation_tap_failed")
        liveCompleted += live
        created(ex, point)
    }

    private fun created(ex: HttpExchange, point: ByteArray) {
        val deviceId = UUID.randomUUID().toString()
        devices.clear() // Q40 (a): the person's other app devices are revoked in the same transaction
        devices[deviceId] = point
        bound = null
        completed++
        json(ex, """{"deviceId":"$deviceId","employeeName":"Maria Borg"}""")
    }

    /**
     * The activation signature over [path] (each route its own text), the strict three-field body,
     * the invitation, the binding: the bound point, or null after the refusal was answered.
     */
    private fun activationSigned(ex: HttpExchange, path: String, body: ByteArray): ByteArray? {
        val id = ex.requestHeaders.getFirst("App-Device-Id")
        val nonce = ex.requestHeaders.getFirst("App-Nonce")
        val sig = ex.requestHeaders.getFirst("App-Signature")?.let(::unb64)
        if (id == null || nonce == null || sig == null || ex.requestHeaders.getFirst("App-Timestamp") != null) {
            unauthenticated(ex)
            return null
        }
        val m = MiniJson.decodeObject(String(body))
        approvalBodies += String(body)
        if (m.keys != setOf("code", "publicKey", "platform") || m["platform"] != "android") {
            error(ex, 400, "bad_request")
            return null
        }
        val point = unb64(m["publicKey"] as String)
        if (point == null || !verifies(point, text("tappa-app-request-v1", "POST", path, body, nonce, id), sig)) {
            unauthenticated(ex)
            return null
        }
        if (m["code"] != code) {
            error(ex, 400, "invalid_invite")
            return null
        }
        val b = bound
        if (b == null || b.activationId != id || b.challenge != nonce || !b.point.contentEquals(point)) {
            error(ex, 409, "activation_not_ready")
            return null
        }
        return point
    }

    private fun approval(ex: HttpExchange, body: ByteArray) {
        activationSigned(ex, "/app/v1/activate/approval", body) ?: return
        approvalFailsOnce?.let { (status, code) ->
            approvalFailsOnce = null
            return error(ex, status, code)
        }
        approvals++
        json(ex, """{"state":"waiting","pairingCode":"$pairingCode","expiresAt":"2026-11-20T10:15:00Z"}""")
    }

    private fun approvalStatus(ex: HttpExchange, body: ByteArray) {
        val point = activationSigned(ex, "/app/v1/activate/approval/status", body) ?: return
        polls++
        when (val next = statusScript.removeFirstOrNull() ?: "waiting") {
            "waiting" -> json(ex, """{"state":"waiting","pairingCode":"$pairingCode","expiresAt":"2026-11-20T10:15:00Z"}""")
            "approved" -> {
                // The device is born in this call, exactly as a successful complete.
                val deviceId = UUID.randomUUID().toString()
                devices.clear()
                devices[deviceId] = point
                bound = null
                completed++
                json(ex, """{"state":"approved","deviceId":"$deviceId","employeeName":"Maria Borg"}""")
            }
            "rejected", "expired" -> json(ex, """{"state":"$next"}""")
            else -> error(ex, next.substringBefore(':').toInt(), next.substringAfter(':'))
        }
    }

    private fun me(ex: HttpExchange, body: ByteArray) {
        if (signedRead(ex, "GET", "/app/v1/me", body) == null) return unauthenticated(ex)
        json(ex, """{"employeeName":"Maria Borg","brand":{},"direction":"in"}""")
    }

    /** The signed read's check (appdevice.VerifyRead): the device id, or null for a 401. */
    private fun signedRead(ex: HttpExchange, method: String, path: String, body: ByteArray): String? {
        val id = ex.requestHeaders.getFirst("App-Device-Id")
        val ts = ex.requestHeaders.getFirst("App-Timestamp")
        val sig = ex.requestHeaders.getFirst("App-Signature")?.let(::unb64)
        val point = id?.let { devices[it] }
        if (ts == null || sig == null || point == null || ex.requestHeaders.getFirst("App-Nonce") != null) return null
        val t = ts.toLongOrNull() ?: return null
        if (abs(t - nowSeconds()) > 60) return null
        if (!verifies(point, text("tappa-app-read-v1", method, path, body, ts, id), sig)) return null
        return id
    }

    // --- My hours (M16-31) --------------------------------------------------------------------

    /** The one week this twin knows, its pages by cursor ("" is page one), and every body sent. */
    var hoursWeek = "2026-11-16"
    val hoursPages = LinkedHashMap<String, String>()
    val hoursBodies = mutableListOf<String>()
    var hoursOff = false

    /** apphours.go's boundary: another week is 400; a cursor not issued for this walk is 400. */
    private fun hours(ex: HttpExchange, body: ByteArray) {
        signedRead(ex, "POST", "/app/v1/hours", body) ?: return unauthenticated(ex)
        hoursBodies += String(body)
        if (hoursOff) return error(ex, 404, "not_found") // the employer turned My hours off (M16-31)
        val m = MiniJson.decodeObject(String(body))
        if (m["week"] != hoursWeek) return error(ex, 400, "bad_request")
        val page = hoursPages[(m["after"] as? String) ?: ""] ?: return error(ex, 400, "bad_request")
        json(ex, page)
    }

    // --- Settings (M16-38): this phone's sign-out and the account deletion request ------------

    /** The path of every revoke / account/delete whose nonce'd signature verified, in order. */
    val forgets = mutableListOf<String>()
    /** When set, the next revoke / account/delete answers this status and error code, once. */
    var forgetFailsOnce: Pair<Int, String>? = null

    /** apptap.go's revoke and appaccount.go's delete: nonce'd, 204, the device gone afterwards. */
    private fun forget(ex: HttpExchange, body: ByteArray, path: String) {
        val id = noncedSigned(ex, path, body) ?: return unauthenticated(ex)
        forgetFailsOnce?.let { (status, code) ->
            forgetFailsOnce = null
            return error(ex, status, code)
        }
        forgets += path
        devices.remove(id)
        ex.sendResponseHeaders(204, -1)
    }

    /** The nonce'd request's check (appdevice.VerifyRequest): the device id, or null for a 401. */
    private fun noncedSigned(ex: HttpExchange, path: String, body: ByteArray): String? {
        val id = ex.requestHeaders.getFirst("App-Device-Id")
        val n = ex.requestHeaders.getFirst("App-Nonce")
        val sig = ex.requestHeaders.getFirst("App-Signature")?.let(::unb64)
        val point = id?.let { devices[it] }
        if (n == null || sig == null || point == null || ex.requestHeaders.getFirst("App-Timestamp") != null) return null
        if (!verifies(point, text("tappa-app-request-v1", "POST", path, body, n, id), sig)) return null
        if (!liveNonces.remove(n)) return null // spent or never issued
        return id
    }

    // --- the tap (M16-13) ---------------------------------------------------------------------

    /** Nonces issued and not yet spent; a nonce-signed request spends its own (single use). */
    val liveNonces = mutableSetOf<String>()
    /** Every tap/start body and every tap body, raw, in order. */
    val tapStartBodies = mutableListOf<String>()
    val tapBodies = mutableListOf<String>()
    /** What tap/start answers about the person. */
    var direction = "in"
    var locationName: String? = "St Julians"
    var tapStartBrand = "{}"
    /** When set, the next tap/start (or tap) answers this status and error code, once. */
    var tapStartFailsOnce: Pair<Int, String>? = null
    var tapFailsOnce: Pair<Int, String>? = null
    /** When set, the next tap is recorded and then its answer is cut (the connection closed). */
    var tapAnswerLostOnce = false
    var recorded = 0
        private set

    private fun nonce(ex: HttpExchange, body: ByteArray) {
        signedRead(ex, "POST", "/app/v1/nonce", body) ?: return unauthenticated(ex)
        val n = b64(ByteArray(32).also(random::nextBytes))
        liveNonces += n
        json(ex, """{"nonce":"$n","expiresAt":"2026-11-18T12:00:00Z"}""")
    }

    private fun tapStart(ex: HttpExchange, body: ByteArray) {
        val id = signedRead(ex, "POST", "/app/v1/tap/start", body) ?: return unauthenticated(ex)
        tapStartBodies += String(body)
        tapStartFailsOnce?.let { (status, code) ->
            tapStartFailsOnce = null
            return error(ex, status, code)
        }
        val m = MiniJson.decodeObject(String(body))
        // Exactly one of the two fields (apptap.go's tapStart / tapStartLive, M16-22).
        if (m.keys != setOf("sun") && m.keys != setOf("liveQR")) return error(ex, 400, "bad_request")
        val loc = locationName?.let { ""","locationName":"$it"""" } ?: ""
        json(ex, """{"employeeName":"Maria Borg"$loc,"brand":$tapStartBrand,"direction":"$direction","qr":false,"context":"ctx-for-$id"}""")
    }

    private fun tap(ex: HttpExchange, body: ByteArray) {
        val id = ex.requestHeaders.getFirst("App-Device-Id")
        val n = ex.requestHeaders.getFirst("App-Nonce")
        val sig = ex.requestHeaders.getFirst("App-Signature")?.let(::unb64)
        val point = id?.let { devices[it] }
        if (n == null || sig == null || point == null || ex.requestHeaders.getFirst("App-Timestamp") != null) return unauthenticated(ex)
        if (!verifies(point, text("tappa-app-request-v1", "POST", "/app/v1/tap", body, n, id), sig)) return unauthenticated(ex)
        if (!liveNonces.remove(n)) return unauthenticated(ex) // spent or never issued
        tapBodies += String(body)
        tapFailsOnce?.let { (status, code) ->
            tapFailsOnce = null
            return error(ex, status, code)
        }
        val m = MiniJson.decodeObject(String(body))
        if (!setOf("context", "want", "lat", "lng", "mockLocation", "occurredAt").containsAll(m.keys)) return error(ex, 400, "bad_request")
        if (m["context"] != "ctx-for-$id") return error(ex, 400, "stale_context")
        recorded++
        if (tapAnswerLostOnce) {
            tapAnswerLostOnce = false
            ex.sendResponseHeaders(200, 1000) // promises a body it never sends: the reply is cut
            ex.responseBody.write("{".toByteArray())
            return
        }
        val d = (m["want"] as? String) ?: direction
        json(ex, """{"outcome":"recorded","verdict":"ok","direction":"$d","venue":"St Julians","at":"14:03","businessType":"restaurant"}""")
    }

    private fun text(prefix: String, method: String, path: String, body: ByteArray, freshness: String, id: String): ByteArray {
        val digest = MessageDigest.getInstance("SHA-256").digest(body).joinToString("") { "%02x".format(it) }
        return "$prefix\n$method\n$path\n$digest\n$freshness\n$id\n".toByteArray(Charsets.US_ASCII)
    }

    private fun json(ex: HttpExchange, s: String) {
        val b = s.toByteArray()
        ex.responseHeaders.set("Content-Type", "application/json; charset=utf-8")
        ex.responseHeaders.set("Cache-Control", "no-store")
        ex.sendResponseHeaders(200, b.size.toLong())
        ex.responseBody.write(b)
    }

    private fun error(ex: HttpExchange, status: Int, code: String) {
        val b = """{"error":"$code"}""".toByteArray()
        ex.responseHeaders.set("Content-Type", "application/json; charset=utf-8")
        ex.sendResponseHeaders(status, b.size.toLong())
        ex.responseBody.write(b)
    }

    private fun unauthenticated(ex: HttpExchange) = ex.sendResponseHeaders(401, -1)

    private fun b64(b: ByteArray) = Base64.getUrlEncoder().withoutPadding().encodeToString(b)

    private fun unb64(s: String): ByteArray? = try {
        Base64.getUrlDecoder().decode(s)
    } catch (_: IllegalArgumentException) {
        null
    }
}
