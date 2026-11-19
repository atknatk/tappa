package mt.taptime.app.wire

// The /app/v1 wire contract (docs/plan/m16-mobil.md, M16-07 "Tel sözleşmesi";
// internal/handler/app.go and appactivate.go). The paths, header names and error vocabulary
// below are pinned to the Go source by ContractPinTest; the response shapes by WireParseTest.
//
// Requests are decoded STRICTLY by the server (an unknown field is 400), so the app sends
// exactly the table's fields with the table's spelling. Responses are read TOLERANTLY: a field
// the server adds later is ignored here, which is the other half of the rule "the server ships
// before any app version that sends a new field".
object Paths {
    const val ACTIVATE_LOOKUP = "/app/v1/activate/lookup"
    const val ACTIVATE_BIND = "/app/v1/activate/bind"
    const val ACTIVATE_COMPLETE = "/app/v1/activate/complete"
    const val NONCE = "/app/v1/nonce"
    const val TAP_START = "/app/v1/tap/start"
    const val TAP = "/app/v1/tap"
    const val ME = "/app/v1/me"
    const val REVOKE = "/app/v1/revoke"
}

object Headers {
    const val DEVICE_ID = "App-Device-Id"
    const val NONCE = "App-Nonce"
    const val TIMESTAMP = "App-Timestamp"
    const val SIGNATURE = "App-Signature"
}

/** The closed error vocabulary of /app/v1 bodies, with each code's one status. */
enum class AppErrorCode(val wire: String, val status: Int) {
    BAD_REQUEST("bad_request", 400),
    INVALID_INVITE("invalid_invite", 400),
    CONSENT_REQUIRED("consent_required", 400),
    INVALID_KEY("invalid_key", 400),
    ACTIVATION_NEEDS_TOUCH("activation_needs_touch", 400),
    ACTIVATION_TAP_FAILED("activation_tap_failed", 400),
    STALE_CONTEXT("stale_context", 400),
    FOREIGN_TENANT("foreign_tenant", 403),
    UNKNOWN_PLAQUE("unknown_plaque", 404),
    NOT_FOUND("not_found", 404),
    METHOD_NOT_ALLOWED("method_not_allowed", 405),
    NEW_KEY_REQUIRED("new_key_required", 409),
    ACTIVATION_NOT_READY("activation_not_ready", 409),
    TRY_AGAIN("try_again", 409),
    RATE_LIMITED("rate_limited", 429),
    SERVER_ERROR("server_error", 500),
    ;

    companion object {
        fun of(wire: String): AppErrorCode? = entries.firstOrNull { it.wire == wire }
    }
}

/** How a call ended, before any route-specific meaning. */
sealed interface Outcome<out T> {
    data class Ok<T>(val value: T) : Outcome<T>
    /** A signed request refused: 401, empty body. Unknown, revoked, bad signature, stale -- one answer. */
    data object Unauthenticated : Outcome<Nothing>
    /** A body from the closed vocabulary. [code] is null for a code this app version does not know. */
    data class Refused(val status: Int, val code: AppErrorCode?) : Outcome<Nothing>
    /** No HTTP answer at all (no network, timeout), or an answer this app cannot read. */
    data class Failed(val reason: String) : Outcome<Nothing>
}

/** The business's two brand slots. [accent] is 6 hex digits without '#'; null fields are Taptime's look. */
data class Brand(val name: String?, val logo: Logo?, val accent: String?) {
    data class Logo(val sha256: String, val width: Int, val height: Int)

    companion object {
        val TAPTIME = Brand(null, null, null)
    }
}

data class LookupReply(
    val employeeName: String,
    val businessName: String,
    val locationName: String?,
    val wifiSsid: String?,
    val secondDevice: Boolean,
    val brand: Brand,
)

data class BindReply(val activationId: String, val challenge: String) {
    // The challenge is signed over (a nonce in all but name): never in a printed value (§7).
    override fun toString() = "BindReply(activationId=$activationId, challenge=<redacted>)"
}

data class CompleteReply(val deviceId: String, val employeeName: String)

data class MeReply(val employeeName: String, val brand: Brand, val direction: String)

data class NonceReply(val nonce: String, val expiresAt: String) {
    override fun toString() = "NonceReply(nonce=<redacted>, expiresAt=$expiresAt)"
}

/**
 * POST /app/v1/tap/start's answer, GET /t's twin: the person, the TAPPED venue (null for a plaque
 * of another business, or none), the brand (Taptime's for another business's plaque), the predicted
 * direction ("in", "out" or "" for the direction-neutral "Tap"), whether the read was a QR one, and
 * the signed tap context POST /app/v1/tap carries back.
 */
data class TapStartReply(
    val employeeName: String,
    val locationName: String?,
    val brand: Brand,
    val direction: String,
    val qr: Boolean,
    val context: String,
) {
    // The signed tap context carries the plaque's uid and counter (tapcontext.go): not printed.
    override fun toString() =
        "TapStartReply(locationName=$locationName, brand=$brand, direction=$direction, qr=$qr, context=<redacted>)"
}

/** POST /app/v1/tap's answer: what the result screen draws. [direction] is "in", "out" or "". */
data class TapReply(
    val outcome: String,
    val verdict: String,
    val direction: String,
    val venue: String,
    val at: String,
    val businessType: String,
    val practice: Boolean,
)

/** The wire's direction words; anything else is the direction-neutral "" (read tolerantly). */
object Direction {
    const val IN = "in"
    const val OUT = "out"

    fun of(s: String?): String = if (s == IN || s == OUT) s else ""

    /** The other direction, "" for none: what the "Not right? …" button offers. */
    fun other(d: String): String = when (d) {
        IN -> OUT
        OUT -> IN
        else -> ""
    }
}

/** Maps decoded objects to the reply types. Pure; every function throws [JsonException] on a shape it cannot read. */
object Replies {
    internal fun brand(f: Fields?): Brand {
        if (f == null) return Brand.TAPTIME
        val logo = f.optObject("logo")?.let { Brand.Logo(it.string("sha256"), it.int("width"), it.int("height")) }
        return Brand(f.optString("name"), logo, f.optString("accent"))
    }

    fun lookup(m: Map<String, Any?>): LookupReply = Fields(m).run {
        LookupReply(
            employeeName = string("employeeName"),
            businessName = string("businessName"),
            locationName = optString("locationName")?.takeIf { it.isNotEmpty() },
            wifiSsid = optString("wifiSsid")?.takeIf { it.isNotEmpty() },
            secondDevice = bool("secondDevice"),
            brand = brand(optObject("brand")),
        )
    }

    fun bind(m: Map<String, Any?>): BindReply = Fields(m).run {
        val id = string("activationId")
        val challenge = string("challenge")
        // The two go into the signed text verbatim; a value the canonical text would refuse is a
        // reply this app cannot use, so it is refused here rather than at the tap.
        if (!Canonical.validDeviceId(id) || !Canonical.validNonce(challenge)) throw JsonException("bind reply: malformed values")
        BindReply(id, challenge)
    }

    fun complete(m: Map<String, Any?>): CompleteReply = Fields(m).run {
        val id = string("deviceId")
        if (!Canonical.validDeviceId(id)) throw JsonException("complete reply: malformed device id")
        CompleteReply(id, string("employeeName"))
    }

    fun me(m: Map<String, Any?>): MeReply = Fields(m).run {
        MeReply(string("employeeName"), brand(optObject("brand")), optString("direction") ?: "")
    }

    fun tapStart(m: Map<String, Any?>): TapStartReply = Fields(m).run {
        val context = string("context")
        // The context goes back verbatim in the signed body; an empty one is a reply this app
        // cannot press with.
        if (context.isEmpty()) throw JsonException("tap start reply: empty context")
        TapStartReply(
            employeeName = string("employeeName"),
            locationName = optString("locationName")?.takeIf { it.isNotEmpty() },
            brand = brand(optObject("brand")),
            direction = Direction.of(optString("direction")),
            qr = optBool("qr"),
            context = context,
        )
    }

    fun tap(m: Map<String, Any?>): TapReply = Fields(m).run {
        TapReply(
            outcome = string("outcome"),
            verdict = string("verdict"),
            direction = Direction.of(optString("direction")),
            venue = optString("venue").orEmpty(),
            at = optString("at").orEmpty(),
            businessType = optString("businessType").orEmpty(),
            practice = optBool("practice"),
        )
    }

    fun nonce(m: Map<String, Any?>): NonceReply = Fields(m).run {
        val n = string("nonce")
        if (!Canonical.validNonce(n)) throw JsonException("nonce reply: malformed nonce")
        NonceReply(n, string("expiresAt"))
    }
}

/** Request bodies, in the contract table's field order and spelling. */
object Bodies {
    const val PLATFORM = "android"

    fun lookup(code: String): ByteArray = JsonObjectWriter().put("code", code).bytes()

    fun bind(code: String, publicPoint: ByteArray): ByteArray =
        JsonObjectWriter().put("code", code).put("consent", true).put("publicKey", B64Url.encode(publicPoint)).bytes()

    fun complete(code: String, publicPoint: ByteArray, sun: String): ByteArray =
        JsonObjectWriter().put("code", code).put("publicKey", B64Url.encode(publicPoint))
            .put("platform", PLATFORM).put("sun", sun).bytes()

    fun tapStart(sun: String): ByteArray = JsonObjectWriter().put("sun", sun).bytes()

    /** tap/start for a live QR (M16-22): exactly one field, the `<base>/q` URL's query as written. */
    fun tapStartLive(liveQuery: String): ByteArray = JsonObjectWriter().put("liveQR", liveQuery).bytes()

    /**
     * POST /app/v1/tap's body. [want] is sent only when the pressed button said a direction; the
     * fix only as a pair; [mockLocation] ONLY WHEN TRUE (card M16-13: the platform's mock flag is a
     * client report that can only tighten -- base:mock-location-review -- and its absence proves
     * nothing, ADR 0045 §2.4, so "false" is never written). occurredAt is never sent: there is no
     * queue (ADR 0045 §2.8).
     */
    fun tap(context: String, want: String?, lat: Double?, lng: Double?, mockLocation: Boolean): ByteArray {
        val w = JsonObjectWriter().put("context", context)
        if (want == Direction.IN || want == Direction.OUT) w.put("want", want)
        if (lat != null && lng != null) w.put("lat", lat).put("lng", lng)
        if (mockLocation && lat != null && lng != null) w.put("mockLocation", true)
        return w.bytes()
    }
}
