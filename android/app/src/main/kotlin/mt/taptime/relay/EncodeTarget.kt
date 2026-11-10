package mt.taptime.relay

import org.json.JSONException
import org.json.JSONObject

/**
 * WHICH BUSINESS A PLAQUE WILL BE ENCODED FOR — known, shown and confirmed before the reader
 * is armed (user decision 2026-11-10; ADR 0017's note of that date).
 *
 * The harm this closes: with the panel's identity sign-in, a person in several businesses
 * lands on the one they used last (ADR 0031 K-4), and the app used to close its sign-in
 * WebView on the first panel page and arm the reader without saying which business that
 * was. A round cannot be undone — its row squats the chip's uid for good and the chip
 * cannot be encoded a second time — so the name is read from the server first
 * (GET /admin/plaques/encode/business) and the reader can only be armed through [arm],
 * which answers only for a name read under THE SAME panel session.
 *
 * WHY THE SESSION, NOT A FLAG. A business switch on the panel issues a NEW session cookie
 * and revokes the old one (ADR 0031 §5.4), so "the cookie the name was read under" is
 * exactly "the business the name belongs to". A name kept across a cookie change is the
 * bug this class exists to make impossible: [arm] and [take] compare the session value
 * every time, and [forget] (called whenever the panel's WebView is opened) drops the name
 * and invalidates any read still in flight.
 *
 * java.* and org.json only (no android.*): the tests drive it against the in-process panel.
 */
class EncodeTarget(private val source: BusinessSource) {
    private var generation = 0L
    private var known: BusinessReply.Known? = null

    /** The panel session cookie VALUE [known] was read under. */
    private var readUnder: String? = null
    private var armed = false

    /**
     * Asks the server which business [cookieHeader]'s session encodes for. Network: call it
     * off the main thread. Whatever was known before is dropped first; a Known answer becomes
     * current only if nothing was forgotten while the request was in flight.
     */
    fun read(cookieHeader: String?): BusinessReply {
        val session = PanelCookie.sessionOf(cookieHeader)
        val gen = synchronized(this) {
            drop()
            generation
        }
        if (cookieHeader == null || session == null) return BusinessReply.NoSession
        val r = source.business(cookieHeader)
        synchronized(this) {
            if (r is BusinessReply.Known && gen == generation) {
                known = r
                readUnder = session
            }
        }
        return r
    }

    /** The business read under exactly this session, or null. Arms nothing. */
    @Synchronized
    fun confirmedFor(cookieHeader: String?): BusinessReply.Known? {
        val k = known ?: return null
        val session = PanelCookie.sessionOf(cookieHeader) ?: return null
        return if (session == readUnder) k else null
    }

    /**
     * Arms the reader for the business read under this session and returns it; null — and
     * disarmed — when none was read under it, or when that business is suspended (the server
     * would refuse every round of it, ADR 0025 §3).
     */
    @Synchronized
    fun arm(cookieHeader: String?): BusinessReply.Known? {
        val k = confirmedFor(cookieHeader)
        if (k == null || k.suspended) {
            armed = false
            return null
        }
        armed = true
        return k
    }

    /**
     * The reader thread's question when a plaque arrives. Disarms whatever the answer, so the
     * same plaque left on the phone cannot start a second round.
     *
     *   [Take.NotArmed]        nobody pressed "Start encoding" (or a round already took it):
     *                          the tap is ignored, the screen is already right.
     *   [Take.SessionChanged]  armed, but the panel cookie is gone or is another session (it
     *                          expired on the phone, or a sign-in happened elsewhere): the name
     *                          on the screen no longer belongs to this cookie. The name is
     *                          dropped and the caller must say so and read again — ignoring the
     *                          tap silently left "Hold the plaque" on screen over a dead session.
     *   [Take.Go]              the business the screen showed, and the exact Cookie header the
     *                          round must carry from its first request to its last.
     */
    @Synchronized
    fun take(cookieHeader: String?): Take {
        if (!armed) return Take.NotArmed
        armed = false
        val k = known
        val session = PanelCookie.sessionOf(cookieHeader)
        if (k == null || cookieHeader == null || session == null || session != readUnder) {
            drop()
            return Take.SessionChanged
        }
        if (k.suspended) return Take.NotArmed
        return Take.Go(k, cookieHeader)
    }

    /** What [take] answers. */
    sealed class Take {
        object NotArmed : Take()
        object SessionChanged : Take()

        /** [cookie] is pinned for the whole round: the round never reads the cookie jar again. */
        class Go(val business: BusinessReply.Known, val cookie: String) : Take() {
            override fun toString(): String = "Go(business=${business.name})"
        }
    }

    @Synchronized
    fun disarm() {
        armed = false
    }

    /** Drops the name and invalidates a read in flight. Called whenever the panel's WebView opens. */
    @Synchronized
    fun forget() {
        drop()
    }

    private fun drop() {
        generation++
        known = null
        readUnder = null
        armed = false
    }
}

/** The business read, as [EncodeTarget] needs it. Declared at the consumer (CLAUDE.md §7). */
interface BusinessSource {
    /** One GET of the business read, carrying exactly [cookieHeader] as the Cookie header. */
    fun business(cookieHeader: String): BusinessReply
}

/** One answer of GET /admin/plaques/encode/business (internal/handler/plaqueencode.go, encodeBusiness). */
sealed class BusinessReply {
    /**
     * 200 with the three-field body. [name] is the session's own business as the panel shell
     * shows it. [canSwitch] says whether the panel's switch screen exists in this deployment
     * — the same for every session, one business or many (the panel's own choice for its
     * "Switch business" link); it is not a membership count. [suspended]: the server refuses
     * every round of this business, so the app starts none. [ref] is the business's reference
     * (the session's tenant id): the round's begin sends it back as `business`, and the server
     * opens no round unless it is the session's own business — so a round can only go to the
     * business this screen showed, and an app that shows nothing (an older APK) sends nothing
     * and is refused.
     */
    data class Known(val name: String, val canSwitch: Boolean, val suspended: Boolean, val ref: String) : BusinessReply()

    /** A JSON `{"fault": word}` — the round routes' vocabulary (encode-unavailable, server-error …). */
    data class Fault(val status: Int, val fault: String) : BusinessReply()

    /** A non-JSON answer from the panel chain: 303 (no session, or the Origin refused), 429. Only the status is read. */
    data class Refused(val status: Int) : BusinessReply()

    data class Malformed(val status: Int, val why: String) : BusinessReply()

    data class Unreachable(val why: String) : BusinessReply()

    /** No panel session cookie on this phone; nothing was asked. */
    object NoSession : BusinessReply()

    companion object {
        /** The body's three field names — encodeBusiness's json tags, pinned by ContractPinTest. */
        const val FIELD_BUSINESS = "business"
        const val FIELD_SWITCH = "switch"
        const val FIELD_SUSPENDED = "suspended"
        const val FIELD_REF = "ref"

        fun parse(status: Int, contentType: String?, body: String): BusinessReply {
            val ct = contentType?.trim()?.lowercase()
            if (ct == null || !ct.startsWith("application/json")) return Refused(status)
            val obj = try {
                JSONObject(body)
            } catch (e: JSONException) {
                return Malformed(status, "body is not a JSON object")
            }
            if (obj.has("fault")) {
                val fault = obj.optString("fault", "")
                if (fault.isEmpty()) return Malformed(status, "empty fault word")
                return Fault(status, fault)
            }
            if (status != 200) return Malformed(status, "non-200 JSON without a fault word")
            val name = obj.opt(FIELD_BUSINESS) as? String
            val canSwitch = obj.opt(FIELD_SWITCH) as? Boolean
            val suspended = obj.opt(FIELD_SUSPENDED) as? Boolean
            val ref = obj.opt(FIELD_REF) as? String
            if (name.isNullOrBlank() || canSwitch == null || suspended == null || ref.isNullOrBlank()) {
                return Malformed(status, "200 body without the four fields")
            }
            return Known(name, canSwitch, suspended, ref)
        }
    }
}

/** The panel session cookie inside a Cookie header (CookieManager hands back every cookie for the path). */
object PanelCookie {
    /** The value of [MainActivity.PANEL_COOKIE] in [header], or null when it is absent or empty. */
    fun sessionOf(header: String?): String? {
        if (header == null) return null
        for (part in header.split(';')) {
            val kv = part.trim()
            val eq = kv.indexOf('=')
            if (eq <= 0) continue
            if (kv.substring(0, eq) == MainActivity.PANEL_COOKIE) {
                return kv.substring(eq + 1).ifEmpty { null }
            }
        }
        return null
    }
}

/**
 * When a page the panel's WebView finished loading ENDS the visit (the WebView closes and
 * the app re-reads the business).
 *
 * The sign-in flow and the switch flow are both visits that pass through several pages, and
 * the WebView must stay open on every one of them: the sign-in page and its "which business"
 * step (/admin/login…), and the switch screen (/admin/switch — including its refusal pages,
 * which are answered at the same address). The visit ends on any OTHER page of the panel's
 * origin once a session cookie exists: the panel 303s to its home after a good sign-in and
 * after a good switch, and "Back to the panel" on the switch screen lands there too.
 */
object PanelPages {
    fun endsVisit(base: String, url: String, hasSession: Boolean): Boolean {
        if (!hasSession) return false
        if (!url.startsWith("$base/")) return false
        val path = url.substring(base.length).substringBefore('?').substringBefore('#')
        if (path.startsWith(HttpRelayServer.LOGIN_PATH)) return false
        if (path == HttpRelayServer.SWITCH_PATH || path.startsWith(HttpRelayServer.SWITCH_PATH + "/")) return false
        return true
    }
}
