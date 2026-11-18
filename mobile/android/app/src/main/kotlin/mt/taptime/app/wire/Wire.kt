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

data class BindReply(val activationId: String, val challenge: String)

data class CompleteReply(val deviceId: String, val employeeName: String)

data class MeReply(val employeeName: String, val brand: Brand, val direction: String)

data class NonceReply(val nonce: String, val expiresAt: String)

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
}
