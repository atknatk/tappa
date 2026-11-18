package mt.taptime.app.wire

import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException

// THE CLOCK-SKEW CONTRACT (card M16-07's review, carried by M16-12 and M16-16). A signed read
// carries the device's Unix second and the server accepts it within 60 s of ITS clock, either
// way; a phone whose clock is further off cannot even get a nonce, and its 401 is the same as a
// revoked device's. So:
//
//  (1) every /app/v1 answer carries a Date header (200, 401, 404 and 405 included -- Go's
//      net/http writes it on every response), and each one re-measures the difference between
//      the server's clock and this phone's; reads are signed with the CORRECTED second;
//  (2) a 401 is read as "signed out" only after ONE retry made with a fresh measurement -- the
//      401's own Date is that measurement (AppClient).
//
// Date has a one-second resolution and is stamped before the response travels, so the measured
// offset is off by up to a second plus the trip: far inside the 60 s window, which is the point.
class ServerClock(private val localMillis: () -> Long = System::currentTimeMillis) {
    @Volatile
    private var offsetMillis: Long = 0

    @Volatile
    var measured: Boolean = false
        private set

    /**
     * Records the server's clock from a Date header value; an unreadable one changes nothing, and
     * so does one more than [MAX_OFFSET_MILLIS] away from this phone's clock (security review,
     * 2026-11-18). A Date is not signed: a captive portal, a proxy or anything on the path can
     * write one, and an offset of days would let a single forged answer set every later signed
     * read's second anywhere. A phone whose real clock is off by more than a day stays
     * uncorrected; its reads are then refused and read as signed out, which is the honest answer.
     */
    fun observe(dateHeader: String?) {
        val server = parseHttpDate(dateHeader) ?: return
        val offset = server - localMillis()
        if (Math.abs(offset) > MAX_OFFSET_MILLIS) return
        offsetMillis = offset
        measured = true
    }

    /** The server's current second as this phone best knows it: the App-Timestamp of a signed read. */
    fun nowSeconds(): Long = Math.floorDiv(localMillis() + offsetMillis, 1000L)

    /** The current offset, server minus phone, in milliseconds. */
    fun offsetMillis(): Long = offsetMillis

    companion object {
        /** The largest server-minus-phone difference a Date may set: one day. */
        const val MAX_OFFSET_MILLIS = 24L * 60 * 60 * 1000

        /** RFC 9110 IMF-fixdate ("Sun, 06 Nov 1994 08:49:37 GMT"), which is what Go writes; null when it is not one. */
        fun parseHttpDate(v: String?): Long? {
            if (v.isNullOrBlank()) return null
            return try {
                ZonedDateTime.parse(v.trim(), DateTimeFormatter.RFC_1123_DATE_TIME).toInstant().toEpochMilli()
            } catch (_: DateTimeParseException) {
                null
            }
        }
    }
}
