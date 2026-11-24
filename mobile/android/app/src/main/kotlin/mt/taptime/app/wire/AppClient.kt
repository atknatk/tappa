package mt.taptime.app.wire

import java.io.IOException

/** A device key that never leaves the phone: it signs, and it shows its public point. */
interface Signer {
    /** The raw uncompressed P-256 point, 65 bytes. */
    val publicPoint: ByteArray

    /** ECDSA P-256 over SHA-256 of [message] (the signer hashes), ASN.1 DER. */
    fun sign(message: ByteArray): ByteArray
}

// THE APP'S ONE CLIENT for /app/v1. It knows the three ways a request is authenticated and
// nothing about what a reply means for a screen:
//
//	unsigned       activate/lookup, activate/bind
//	activation     activate/complete, activate/approval, activate/approval/status (M16-23):
//	               App-Device-Id = the activation id, App-Nonce = the server's challenge, signed
//	               by the BOUND key (the nonce-signed text's format)
//	signed read    /me, /tap/start, /nonce: App-Timestamp = the corrected Unix second
//	nonce-signed   /tap, /revoke: a fresh nonce from /nonce, then App-Nonce
//
// Every reply's Date goes to the clock first (ServerClock, contract (1)). A 401 to a signed read
// or a nonce-signed request is retried ONCE -- after the 401's own Date has re-measured the clock,
// and for a nonce-signed request with a fresh nonce -- and only the second 401 comes back as
// Unauthenticated (contract (2)). activate/complete is not retried: its text carries no time, and
// a 401 there is the proof of possession refused.
//
// NOTHING HERE LOGS. A signature, a nonce, a challenge, the invite code, the SUN query, a live QR
// query and a pairing code all pass through this class; none is written anywhere but the request
// (CLAUDE.md §7).
class AppClient(
    private val transport: Transport,
    /** Read by the flows for [ServerClock.lastAnswerWasFarOff]: a 401 then means the phone's clock. */
    val clock: ServerClock,
    private val json: JsonDecoder,
) {
    // --- unsigned ---------------------------------------------------------------------------

    fun lookup(code: String): Outcome<LookupReply> =
        call("POST", Paths.ACTIVATE_LOOKUP, emptyMap(), Bodies.lookup(code), Replies::lookup)

    fun bind(code: String, key: Signer): Outcome<BindReply> =
        call("POST", Paths.ACTIVATE_BIND, emptyMap(), Bodies.bind(code, key.publicPoint), Replies::bind)

    // --- activation -------------------------------------------------------------------------

    fun complete(code: String, bound: BindReply, key: Signer, sun: String): Outcome<CompleteReply> =
        activation(Paths.ACTIVATE_COMPLETE, Bodies.complete(code, key.publicPoint, sun), bound, key, Replies::complete)

    /** complete with a venue screen's live QR (M16-23): the `<base>/q` URL's query as `liveQR`. */
    fun completeLive(code: String, bound: BindReply, key: Signer, liveQuery: String): Outcome<CompleteReply> =
        activation(Paths.ACTIVATE_COMPLETE, Bodies.completeLive(code, key.publicPoint, liveQuery), bound, key, Replies::complete)

    /** Opens a manager-approval request (M16-23); an earlier open one of the invitation is withdrawn. */
    fun approval(code: String, bound: BindReply, key: Signer): Outcome<ApprovalReply> =
        activation(Paths.ACTIVATE_APPROVAL, Bodies.approval(code, key.publicPoint), bound, key, Replies::approval)

    /** The approval request's state; "approved" means the device was created in this call. */
    fun approvalStatus(code: String, bound: BindReply, key: Signer): Outcome<ApprovalReply> =
        activation(Paths.ACTIVATE_APPROVAL_STATUS, Bodies.approval(code, key.publicPoint), bound, key, Replies::approval)

    // The activation signature: App-Device-Id = the activation id, App-Nonce = the server's
    // challenge, signed by the BOUND key over [path] -- each route's own text. Not retried.
    private fun <T> activation(
        path: String, body: ByteArray, bound: BindReply, key: Signer, parse: (Map<String, Any?>) -> T,
    ): Outcome<T> {
        val text = Canonical.request("POST", path, body, bound.challenge, bound.activationId)
        val headers = mapOf(
            Headers.DEVICE_ID to bound.activationId,
            Headers.NONCE to bound.challenge,
            Headers.SIGNATURE to B64Url.encode(key.sign(text)),
        )
        return call("POST", path, headers, body, parse)
    }

    // --- signed ------------------------------------------------------------------------------

    fun me(deviceId: String, key: Signer): Outcome<MeReply> =
        signedRead("GET", Paths.ME, null, deviceId, key, Replies::me)

    /** GET /t's twin: a signed read, so it spends no nonce and advances no counter. */
    fun tapStart(deviceId: String, key: Signer, sun: String): Outcome<TapStartReply> =
        signedRead("POST", Paths.TAP_START, Bodies.tapStart(sun), deviceId, key, Replies::tapStart)

    /** tap/start for a venue screen's live QR (M16-22): the `<base>/q` URL's query as `liveQR`. */
    fun tapStartLive(deviceId: String, key: Signer, liveQuery: String): Outcome<TapStartReply> =
        signedRead("POST", Paths.TAP_START, Bodies.tapStartLive(liveQuery), deviceId, key, Replies::tapStart)

    /**
     * POST /api/checkin's twin, nonce-signed. An empty 2xx is an answer this app cannot read: the
     * record may exist, so it is [Outcome.Failed], never Ok.
     */
    fun tap(deviceId: String, key: Signer, body: ByteArray): Outcome<TapReply> =
        when (val r = signedRequest("POST", Paths.TAP, body, deviceId, key, Replies::tap)) {
            is Outcome.Ok -> r.value?.let { Outcome.Ok(it) } ?: Outcome.Failed("empty reply")
            is Outcome.Refused -> r
            is Outcome.Failed -> r
            Outcome.Unauthenticated -> Outcome.Unauthenticated
        }

    fun claims(deviceId: String, key: Signer): Outcome<ClaimsReply> =
        signedRead("POST", Paths.CLAIMS, Bodies.claims(), deviceId, key, Replies::claims)

    fun hours(deviceId: String, key: Signer, week: String?, after: String?): Outcome<HoursReply> =
        signedRead("POST", Paths.HOURS, Bodies.hours(week, after), deviceId, key, Replies::hours)

    fun claimSend(deviceId: String, key: Signer, context: String, at: String): Outcome<String> =
        when (val r = signedRequest("POST", Paths.CLAIMS_SEND, Bodies.claimSend(context, at), deviceId, key, Replies::claimSend)) {
            is Outcome.Ok -> r.value?.let { Outcome.Ok(it) } ?: Outcome.Failed("empty reply")
            is Outcome.Refused -> r
            is Outcome.Failed -> r
            Outcome.Unauthenticated -> Outcome.Unauthenticated
        }

    fun nonce(deviceId: String, key: Signer): Outcome<NonceReply> =
        signedRead("POST", Paths.NONCE, ByteArray(0), deviceId, key, Replies::nonce)

    /** A signed read: [body] null sends none (GET); its digest is then the empty body's. */
    fun <T> signedRead(
        method: String, path: String, body: ByteArray?, deviceId: String, key: Signer, parse: (Map<String, Any?>) -> T,
    ): Outcome<T> {
        var last: Outcome<T> = Outcome.Unauthenticated
        repeat(2) {
            val ts = clock.nowSeconds().toString()
            val text = Canonical.read(method, path, body ?: ByteArray(0), ts, deviceId)
            val headers = mapOf(
                Headers.DEVICE_ID to deviceId,
                Headers.TIMESTAMP to ts,
                Headers.SIGNATURE to B64Url.encode(key.sign(text)),
            )
            last = call(method, path, headers, body, parse)
            if (last != Outcome.Unauthenticated) return last
        }
        return last
    }

    /** A nonce-signed request: a fresh nonce each attempt; a 204 or empty 200 answers Ok(null). */
    fun <T> signedRequest(
        method: String, path: String, body: ByteArray, deviceId: String, key: Signer, parse: (Map<String, Any?>) -> T,
    ): Outcome<T?> {
        var last: Outcome<T?> = Outcome.Unauthenticated
        repeat(2) {
            val n = when (val issued = nonce(deviceId, key)) {
                is Outcome.Ok -> issued.value.nonce
                is Outcome.Refused -> return issued
                is Outcome.Failed -> return issued
                Outcome.Unauthenticated -> return Outcome.Unauthenticated // already retried once
            }
            val text = Canonical.request(method, path, body, n, deviceId)
            val headers = mapOf(
                Headers.DEVICE_ID to deviceId,
                Headers.NONCE to n,
                Headers.SIGNATURE to B64Url.encode(key.sign(text)),
            )
            last = callMaybeEmpty(method, path, headers, body, parse)
            if (last != Outcome.Unauthenticated) return last
        }
        return last
    }

    // --- one exchange ------------------------------------------------------------------------

    private fun <T> call(
        method: String, path: String, headers: Map<String, String>, body: ByteArray?, parse: (Map<String, Any?>) -> T,
    ): Outcome<T> = when (val r = callMaybeEmpty(method, path, headers, body, parse)) {
        is Outcome.Ok -> r.value?.let { Outcome.Ok(it) } ?: Outcome.Failed("empty reply")
        is Outcome.Refused -> r
        is Outcome.Failed -> r
        Outcome.Unauthenticated -> Outcome.Unauthenticated
    }

    private fun <T> callMaybeEmpty(
        method: String, path: String, headers: Map<String, String>, body: ByteArray?, parse: (Map<String, Any?>) -> T,
    ): Outcome<T?> {
        val reply = try {
            transport.exchange(method, path, headers, body)
        } catch (e: IOException) {
            return Outcome.Failed("no answer: ${e.javaClass.simpleName}")
        }
        clock.observe(reply.date)
        return when {
            reply.status == 401 -> Outcome.Unauthenticated
            reply.status in 200..299 -> {
                if (reply.body.isEmpty()) return Outcome.Ok(null)
                try {
                    Outcome.Ok(parse(json.decodeObject(String(reply.body, Charsets.UTF_8))))
                } catch (e: JsonException) {
                    Outcome.Failed("unreadable reply: ${e.message}")
                }
            }
            else -> Outcome.Refused(reply.status, errorCode(reply.body))
        }
    }

    private fun errorCode(body: ByteArray): AppErrorCode? = try {
        (json.decodeObject(String(body, Charsets.UTF_8))["error"] as? String)?.let(AppErrorCode::of)
    } catch (_: JsonException) {
        null
    }
}
