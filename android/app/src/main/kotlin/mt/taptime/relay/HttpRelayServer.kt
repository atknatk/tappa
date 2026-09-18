package mt.taptime.relay

import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.net.URLEncoder

/**
 * The real transport: three form-encoded POSTs over java.net.HttpURLConnection.
 *
 * TWO HEADERS ARE THE CONTRACT (plaqueencode.go, "THE RELAY MUST SEND AN Origin
 * HEADER"; m8-deploy-pilot.md open item 20):
 *
 *   Origin   the panel's own origin. The three routes mount under
 *            AdminAuth.ProtectWriting, whose sameOriginGate refuses a request that
 *            carries neither an Origin equal to TAPPA_BASE_URL nor a
 *            Sec-Fetch-Site of same-origin/same-site — and a native client sends
 *            neither by default. Measured on the server (TestPlaqueEncode_
 *            TheRelayMustDeclareItsOrigin): without it the round never opens.
 *   Cookie   the panel session, as the WebView sign-in left it in CookieManager.
 *            This app invents no identity surface (ADR 0017 §6 md. 10): the person
 *            encoding is the signed-in panel admin, and the tenant is theirs.
 *
 * Redirects are NOT followed. The chain answers refusals with a 303 (sameOriginGate
 * to /admin, requireAdmin to /admin/login) and following one would turn a refusal
 * into a GET of a sign-in page that parses as nothing. The status is the signal.
 *
 * java.* only, no android.* import: the tests run this class unchanged against an
 * in-process HTTP server and assert the headers on every route.
 */
class HttpRelayServer(
    base: String,
    /** Returns the Cookie header value for the panel, or null when signed out. */
    private val cookie: () -> String?,
    /**
     * Whether a plain-http address is acceptable. The app passes BuildConfig.DEBUG
     * (see MainActivity); the tests pass true because the in-process panel is http.
     * Defaults to FALSE so that a caller who forgets gets the strict side.
     */
    allowHttp: Boolean = false,
    private val connectTimeoutMs: Int = 10_000,
    private val readTimeoutMs: Int = 20_000,
) : RelayServer {
    private val base: String = normaliseBase(base, allowHttp)

    /** The Origin header this client sends: scheme and authority of [base], nothing else. */
    val origin: String = originOf(this.base)

    override fun begin(): Reply = post(ENCODE_PATH, emptyMap())

    override fun step(session: String, rapdu: ByteArray): Reply =
        post(STEP_PATH, mapOf("session" to session, "rapdu" to Hex.encode(rapdu)))

    override fun abort(session: String): Reply = post(ABORT_PATH, mapOf("session" to session))

    /**
     * Signs the panel session out on the server (POST /admin/logout, same chain shape:
     * Origin + Cookie). Returns the HTTP status, or -1 when unreachable. The caller
     * clears the cookie jar afterwards whatever the answer — a cookie the server may
     * still honour must not stay on the phone.
     */
    fun signOut(): Int {
        return try {
            open(LOGOUT_PATH).use { c ->
                c.write(ByteArray(0))
                c.responseCode
            }
        } catch (e: IOException) {
            -1
        }
    }

    /**
     * Asks whether the cookie jar still holds a live panel session: a GET of the panel
     * with no redirect-following. 200 means signed in; a 303 means not. Costs one
     * request of the panel's session budget.
     */
    fun probeSignedIn(): Boolean {
        return try {
            val c = URL(base + PANEL_PATH).openConnection() as HttpURLConnection
            try {
                c.requestMethod = "GET"
                c.instanceFollowRedirects = false
                c.useCaches = false
                c.connectTimeout = connectTimeoutMs
                c.readTimeout = readTimeoutMs
                cookie()?.let { c.setRequestProperty("Cookie", it) }
                c.responseCode == 200
            } finally {
                c.disconnect()
            }
        } catch (e: IOException) {
            false
        }
    }

    /**
     * The signed-out probe: one POST at the encode route with Origin and NO cookie,
     * answered by the chain before any round can open. What comes back tells the
     * operator whether the address is right, and tells this app whether the phone
     * really put Origin on the wire:
     *
     *     303 to /admin/login   Origin accepted, no session   -> the address is right
     *     303 to /admin         sameOriginGate refused        -> Origin missing or wrong
     *
     * It can never open a round: the cookie is not sent, so requireAdmin answers
     * before the handler runs (m8-deploy-pilot.md item 20 measured this on the server).
     */
    fun probeOrigin(): Probe {
        return try {
            val c = URL(base + ENCODE_PATH).openConnection() as HttpURLConnection
            try {
                c.requestMethod = "POST"
                c.doOutput = true
                c.instanceFollowRedirects = false
                c.useCaches = false
                c.connectTimeout = connectTimeoutMs
                c.readTimeout = readTimeoutMs
                c.setRequestProperty("Origin", origin)
                c.setRequestProperty("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
                c.setRequestProperty("User-Agent", USER_AGENT)
                c.setFixedLengthStreamingMode(0)
                c.outputStream.use { }
                Probe(c.responseCode, c.getHeaderField("Location"))
            } finally {
                c.disconnect()
            }
        } catch (e: IOException) {
            Probe(-1, e.message ?: e.javaClass.simpleName)
        }
    }

    /** What [probeOrigin] saw: the status, and the Location header (or the failure text when status is -1). */
    data class Probe(val status: Int, val location: String?) {
        val originAccepted: Boolean get() = status == 303 && location?.endsWith(LOGIN_PATH) == true
        val originRefused: Boolean get() = status == 303 && !originAccepted

        fun describe(): String = when {
            originAccepted -> "Server reachable and this app's origin is accepted. Sign in to continue."
            originRefused -> "Server reachable but it refused this app's origin — the address must equal the server's own TAPPA_BASE_URL."
            status == -1 -> "Could not reach the server ($location)."
            status == 429 -> "Server reachable but its request budget for this address is spent. Wait ten minutes."
            else -> "Server answered HTTP $status" + (if (location != null) " (to $location)" else "") + "."
        }
    }

    private fun post(path: String, form: Map<String, String>): Reply {
        val body = encodeForm(form)
        return try {
            open(path).use { c ->
                c.write(body)
                val status = c.responseCode
                val text = readAll(if (status < 400) c.inputStream else c.errorStream)
                Reply.parse(status, c.getHeaderField("Content-Type"), text)
            }
        } catch (e: IOException) {
            Reply.Unreachable(e.message ?: e.javaClass.simpleName)
        }
    }

    private class Conn(val c: HttpURLConnection) : AutoCloseable {
        val responseCode: Int get() = c.responseCode
        val inputStream: InputStream? get() = c.inputStream
        val errorStream: InputStream? get() = c.errorStream
        fun getHeaderField(name: String): String? = c.getHeaderField(name)
        fun write(body: ByteArray) {
            c.setFixedLengthStreamingMode(body.size)
            c.outputStream.use { it.write(body) }
        }
        override fun close() = c.disconnect()
    }

    private fun open(path: String): Conn {
        val c = URL(base + path).openConnection() as HttpURLConnection
        c.requestMethod = "POST"
        c.doOutput = true
        c.instanceFollowRedirects = false
        c.useCaches = false
        c.connectTimeout = connectTimeoutMs
        c.readTimeout = readTimeoutMs
        c.setRequestProperty("Origin", origin)
        c.setRequestProperty("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
        c.setRequestProperty("Accept", "application/json")
        c.setRequestProperty("User-Agent", USER_AGENT)
        cookie()?.let { c.setRequestProperty("Cookie", it) }
        return Conn(c)
    }

    companion object {
        const val ENCODE_PATH = "/admin/plaques/encode"
        const val STEP_PATH = "/admin/plaques/encode/step"
        const val ABORT_PATH = "/admin/plaques/encode/abort"
        const val LOGOUT_PATH = "/admin/logout"
        const val PANEL_PATH = "/admin"
        const val LOGIN_PATH = "/admin/login"
        const val USER_AGENT = "TaptimeRelay/0.1 (Android)"

        /**
         * Canonical form of the server address: lower-case scheme + lower-case host
         * [+ port], no userinfo, no path, no trailing slash. Throws on anything else,
         * because the address doubles as the Origin header and the server compares
         * that string with TAPPA_BASE_URL (adminlogin.go, sameOrigin): an address
         * with a path could never match.
         *
         * LOWER-CASE IS LOAD-BEARING, NOT COSMETIC (security audit, F5). The server's
         * Origin check is case-insensitive, but the WebView is not: Chromium reports
         * every navigation with a lower-case host, and MainActivity decides "is this
         * still the panel" with startsWith(base). With `https://TapTime.mt` typed by
         * the operator the first version kept the case, the post-sign-in 303 to
         * /admin was refused as off-origin, and sign-in could never complete. The
         * round-1 test hid it by lower-casing its own input.
         *
         * [allowHttp] is the cleartext gate (audit F4): before it, `http://` was
         * accepted here and refused only by the platform's cleartext policy — the
         * right outcome for the wrong reason. A release build refuses it HERE.
         */
        fun normaliseBase(raw: String, allowHttp: Boolean): String {
            val u = try {
                URI(raw.trim())
            } catch (e: Exception) {
                throw IllegalArgumentException("not a URL")
            }
            val scheme = u.scheme?.lowercase()
            require(scheme == "https" || scheme == "http") { "the address must start with https://" + if (allowHttp) " or http://" else "" }
            require(scheme == "https" || allowHttp) { "plain http:// is only accepted by a debug build" }
            require(!u.host.isNullOrEmpty()) { "the address has no host" }
            require(u.userInfo == null) { "the address must not carry credentials" }
            require(u.path.isNullOrEmpty() || u.path == "/") { "the address must be the server's origin only, without a path" }
            require(u.query == null && u.fragment == null) { "the address must not carry a query or fragment" }
            val host = u.host.lowercase()
            val port = if (u.port == -1) "" else ":${u.port}"
            return "$scheme://$host$port"
        }

        fun originOf(normalisedBase: String): String = normalisedBase

        private fun encodeForm(form: Map<String, String>): ByteArray {
            val sb = StringBuilder()
            for ((k, v) in form) {
                if (sb.isNotEmpty()) sb.append('&')
                sb.append(URLEncoder.encode(k, "UTF-8")).append('=').append(URLEncoder.encode(v, "UTF-8"))
            }
            return sb.toString().toByteArray(Charsets.UTF_8)
        }

        private fun readAll(s: InputStream?): String {
            if (s == null) return ""
            return s.use { String(it.readBytes(), Charsets.UTF_8) }
        }
    }
}
