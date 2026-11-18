package mt.taptime.app.tap

/**
 * One position reading taken at the press (CLAUDE.md §4.2), and the platform's mock flag for THAT
 * reading. It exists in memory for the length of one POST /app/v1/tap and is never stored or logged.
 */
class Fix(val lat: Double, val lng: Double, val mock: Boolean)

// THE RULES OF THE ONE READING, pure so the JVM tests drive them; TapLocation.kt is the platform
// half that asks LocationManager once. Every rule is the browser's (web/static/js/tap.js) unless it
// says otherwise.
object FixRules {
    /**
     * How long the press waits for a reading before it goes without one: tap.js's FIX_TIMEOUT_MS. A
     * tap that goes through without coordinates is a tap; a tap that never goes through is a
     * missing hour.
     */
    const val TIMEOUT_MILLIS = 8_000L

    // LocationManager's provider names (FUSED_PROVIDER is API 31+, the platform's own fused
    // provider -- AOSP, not Google Play services' Fused Location Provider, which Q36 does not
    // carry). Pinned to the platform constants in FixRulesTest.
    const val FUSED = "fused"
    const val GPS = "gps"
    const val NETWORK = "network"

    /**
     * Which provider the one reading asks, or null for none (the press then goes without a fix):
     *  * API 31+: the platform's fused provider when it is there and on -- it works with the coarse
     *    permission too (Android 12's "approximate" choice);
     *  * otherwise GPS, which needs the FINE permission (tap.js asks for high accuracy);
     *  * otherwise the network provider, with either permission.
     * [enabled] is LocationManager.isProviderEnabled; a provider the phone does not have is "off".
     */
    fun provider(sdk: Int, fine: Boolean, coarse: Boolean, enabled: (String) -> Boolean): String? {
        if (!fine && !coarse) return null
        if (sdk >= 31 && enabled(FUSED)) return FUSED
        if (fine && enabled(GPS)) return GPS
        if (enabled(NETWORK)) return NETWORK
        return null
    }

    /**
     * The reading the press sends, or null. A reading taken BEFORE the press is refused: tap.js
     * asks with maximumAge 0 -- "a remembered position is exactly the 'where you were' signal §4.2
     * exists to keep out of this product" -- and the platform's getCurrentLocation (API 30+) may
     * answer with a recent cached one. [fixNanos] and [pressNanos] are SystemClock's
     * elapsedRealtimeNanos (the reading's own, and the press's): one monotonic clock, so a wall
     * clock change cannot move either. A coordinate out of range or not finite is no reading
     * (the server's appFix would drop it anyway).
     */
    fun accept(lat: Double, lng: Double, mock: Boolean, fixNanos: Long, pressNanos: Long): Fix? {
        if (fixNanos < pressNanos) return null
        if (!lat.isFinite() || !lng.isFinite() || lat < -90 || lat > 90 || lng < -180 || lng > 180) return null
        return Fix(lat, lng, mock)
    }
}
