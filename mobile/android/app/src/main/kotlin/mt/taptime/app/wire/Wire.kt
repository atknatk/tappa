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
    /** M16-23: a manager-approval request for a phone that cannot tap (signed like complete). */
    const val ACTIVATE_APPROVAL = "/app/v1/activate/approval"
    /** M16-23: the request's state; an approved one is collected here (the device is born in this call). */
    const val ACTIVATE_APPROVAL_STATUS = "/app/v1/activate/approval/status"
    const val NONCE = "/app/v1/nonce"
    const val TAP_START = "/app/v1/tap/start"
    const val TAP = "/app/v1/tap"
    const val ME = "/app/v1/me"
    const val REVOKE = "/app/v1/revoke"
    /** M16-43: the records that take a claim now and the person's own claims (a signed read). */
    const val CLAIMS = "/app/v1/claims"
    /** M16-43: one claim (nonce-signed). */
    const val CLAIMS_SEND = "/app/v1/claims/send"
    /** M16-31: the person's own week (a signed read). */
    const val HOURS = "/app/v1/hours"
    /** M16-37: "Delete my account" (nonce-signed, 204). */
    const val ACCOUNT_DELETE = "/app/v1/account/delete"
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
    /** M16-23: a live QR screen runs at the workplace -- scan it; the manager cannot approve this phone. */
    SCREEN_AVAILABLE("screen_available", 409),
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
    /**
     * M16-23: the manager-approval path is offered -- no live QR screen runs at the person's
     * workplace. Absent is false (an older server offers nothing).
     */
    val managerApproval: Boolean = false,
)

data class BindReply(val activationId: String, val challenge: String) {
    // The challenge is signed over (a nonce in all but name): never in a printed value (§7).
    override fun toString() = "BindReply(activationId=$activationId, challenge=<redacted>)"
}

data class CompleteReply(val deviceId: String, val employeeName: String)

/**
 * M16-23: POST /app/v1/activate/approval and approval/status. [Approved] is a finished activation:
 * the server created the device in that very call, exactly as a successful complete.
 */
sealed interface ApprovalReply {
    /** [pairingCode] is six ASCII digits the manager matches in the panel: never printed (§7). */
    class Waiting(val pairingCode: String, val expiresAt: String) : ApprovalReply {
        override fun toString() = "Waiting(pairingCode=<redacted>, expiresAt=$expiresAt)"
        override fun equals(other: Any?) = other is Waiting && other.pairingCode == pairingCode && other.expiresAt == expiresAt
        override fun hashCode() = pairingCode.hashCode() * 31 + expiresAt.hashCode()
    }
    data class Approved(val deviceId: String, val employeeName: String) : ApprovalReply
    data object Rejected : ApprovalReply
    data object Expired : ApprovalReply
}

/**
 * GET /app/v1/me. [presence] is the open check-in the next tap would close (M16-45; null when there
 * is none), [shift] the usual shift (null unless the owner marked shifts real), [missingTimes] how
 * many records take a claim now (M16-43). An old server omits the three: null, null, 0.
 */
data class MeReply(
    val employeeName: String, val brand: Brand, val direction: String,
    val presence: Presence? = null, val shift: UsualShift? = null, val missingTimes: Int = 0,
    /** M16-30: the owner's My hours window in weeks; 0 is off (and an old server's answer). */
    val hoursWeeks: Int = 0,
    /** M16-37: the employer's name, always set by a current server ("" from an older one). */
    val businessName: String = "",
)

/**
 * The server's instants (epoch milliseconds) for the phone's scheduling, and its wall clocks
 * ("HH:MM", the business's zone) for the words. The app converts no zone (§6).
 */
data class Presence(
    val inSinceMs: Long, val inSinceLocal: String,
    val shiftEndsAtMs: Long?, val shiftEndsAtLocal: String?,
    val remindAtMs: Long?, val showUntilMs: Long, val staleAtMs: Long,
)

data class UsualShift(val start: String, val end: String, val overnight: Boolean, val venue: String)

/** One record that takes a claim now; [context] is signed for this device and goes back verbatim. */
data class OpenClaim(
    val context: String, val kind: String, val anchorDate: String, val anchorAt: String, val day: String,
    val windowFrom: String, val fromInclusive: Boolean, val windowTo: String, val toInclusive: Boolean,
    val prefill: String?,
) {
    override fun toString() = "OpenClaim(context=<redacted>, kind=$kind, day=$day)"
}

data class SentClaim(
    val kind: String, val anchorDate: String, val anchorAt: String, val claimedAt: String,
    val state: String, val approvedAt: String?,
)

data class ClaimsReply(val open: List<OpenClaim>, val sent: List<SentClaim>)

/** M16-31: one week of My hours. Minutes are whole and floored by the server; dates and clocks are the business's. */
data class HoursTotals(
    val workedMinutes: Int, val awaitingMinutes: Int, val openEntries: Int, val checkInMissing: Int,
    val truncated: Boolean, val complete: Boolean,
)

data class HoursDay(val date: String, val workedMinutes: Int)

data class HoursRecord(
    val date: String, val at: String, val venue: String, val direction: String, val channel: String,
    val state: String, val claim: String?,
)

data class HoursReply(
    val week: String, val weeksVisible: Int, val hasPrev: Boolean, val hasNext: Boolean,
    val totals: HoursTotals, val days: List<HoursDay>, val records: List<HoursRecord>,
    val nextCursor: String?, val waitingRecords: Int, val waitingClaims: Int,
)

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
            managerApproval = optBool("managerApproval"),
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

    /** The six digits the panel shows: anything else is a reply this app cannot show. */
    private val pairing = Regex("^[0-9]{6}$")

    fun approval(m: Map<String, Any?>): ApprovalReply = Fields(m).run {
        when (string("state")) {
            "waiting" -> {
                val c = string("pairingCode")
                if (!pairing.matches(c)) throw JsonException("approval reply: malformed pairing code")
                ApprovalReply.Waiting(c, optString("expiresAt").orEmpty())
            }
            "approved" -> {
                val id = string("deviceId")
                if (!Canonical.validDeviceId(id)) throw JsonException("approval reply: malformed device id")
                ApprovalReply.Approved(id, string("employeeName"))
            }
            "rejected" -> ApprovalReply.Rejected
            "expired" -> ApprovalReply.Expired
            else -> throw JsonException("approval reply: unknown state")
        }
    }

    fun me(m: Map<String, Any?>): MeReply = Fields(m).run {
        MeReply(
            string("employeeName"), brand(optObject("brand")), optString("direction") ?: "",
            presence = optObject("presence")?.let(::presence),
            shift = optObject("shift")?.run { UsualShift(string("start"), string("end"), optBool("overnight"), optString("venue") ?: "") },
            missingTimes = optInt("missingTimes"),
            hoursWeeks = optInt("hoursWeeks"),
            businessName = optString("businessName").orEmpty(),
        )
    }

    private fun presence(f: Fields): Presence = f.run {
        Presence(
            inSinceMs = instant(string("inSince")), inSinceLocal = string("inSinceLocal"),
            shiftEndsAtMs = optString("shiftEndsAt")?.let(::instant), shiftEndsAtLocal = optString("shiftEndsAtLocal"),
            remindAtMs = optString("remindAt")?.let(::instant),
            showUntilMs = instant(string("showUntil")), staleAtMs = instant(string("staleAt")),
        )
    }

    // RFC 3339 in UTC as the server writes it (Go's time.Time JSON, "Z").
    private fun instant(v: String): Long = try {
        java.time.Instant.parse(v).toEpochMilli()
    } catch (_: java.time.format.DateTimeParseException) {
        throw JsonException("an instant that does not parse")
    }

    fun claims(m: Map<String, Any?>): ClaimsReply = Fields(m).run {
        ClaimsReply(
            open = objects("open").map { o ->
                o.run {
                    val from = optObject("windowFrom") ?: throw JsonException("claim: no window")
                    val to = optObject("windowTo") ?: throw JsonException("claim: no window")
                    OpenClaim(
                        string("context"), string("kind"), string("anchorDate"), string("anchorAt"), string("day"),
                        from.string("at"), from.optBool("inclusive"), to.string("at"), to.optBool("inclusive"),
                        optString("prefill"),
                    )
                }
            },
            sent = objects("sent").map { o ->
                o.run { SentClaim(string("kind"), string("anchorDate"), string("anchorAt"), string("claimedAt"), string("state"), optString("approvedAt")) }
            },
        )
    }

    fun claimSend(m: Map<String, Any?>): String = Fields(m).string("result")

    fun hours(m: Map<String, Any?>): HoursReply = Fields(m).run {
        val t = optObject("totals") ?: throw JsonException("hours: no totals")
        val w = optObject("waiting")
        HoursReply(
            week = string("week"), weeksVisible = int("weeksVisible"),
            hasPrev = optBool("hasPrev"), hasNext = optBool("hasNext"),
            totals = HoursTotals(
                t.int("workedMinutes"), t.optInt("awaitingMinutes"), t.optInt("openEntries"), t.optInt("checkInMissing"),
                t.optBool("truncated"), t.optBool("complete"),
            ),
            days = objects("days").map { d -> HoursDay(d.string("date"), d.int("workedMinutes")) },
            records = objects("records").map { r ->
                HoursRecord(
                    r.string("date"), r.string("at"), r.optString("venue").orEmpty(), r.optString("direction").orEmpty(),
                    r.optString("channel") ?: "other", r.string("state"), r.optString("claim"),
                )
            },
            nextCursor = optString("nextCursor"),
            waitingRecords = w?.optInt("records") ?: 0,
            waitingClaims = w?.optInt("claims") ?: 0,
        )
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

    /**
     * activate/complete with a venue screen's LIVE QR (M16-23): the `<base>/q` URL's query as
     * `liveQR` in place of `sun` -- exactly one of the two, as on tap/start.
     */
    fun completeLive(code: String, publicPoint: ByteArray, liveQuery: String): ByteArray =
        JsonObjectWriter().put("code", code).put("publicKey", B64Url.encode(publicPoint))
            .put("platform", PLATFORM).put("liveQR", liveQuery).bytes()

    /** activate/approval and approval/status (M16-23): the same three fields. */
    fun approval(code: String, publicPoint: ByteArray): ByteArray =
        JsonObjectWriter().put("code", code).put("publicKey", B64Url.encode(publicPoint)).put("platform", PLATFORM).bytes()

    fun tapStart(sun: String): ByteArray = JsonObjectWriter().put("sun", sun).bytes()

    /** POST /app/v1/claims: an empty object. */
    fun claims(): ByteArray = "{}".toByteArray(Charsets.UTF_8)

    /** POST /app/v1/hours: a Monday "YYYY-MM-DD" (null: this week) and the previous page's cursor. */
    fun hours(week: String?, after: String?): ByteArray {
        val w = JsonObjectWriter()
        if (week != null) w.put("week", week)
        if (after != null) w.put("after", after)
        return w.bytes()
    }

    /** POST /app/v1/claims/send: the signed context as it came, and the wall clock "HH:MM". */
    fun claimSend(context: String, at: String): ByteArray = JsonObjectWriter().put("context", context).put("at", at).bytes()

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
