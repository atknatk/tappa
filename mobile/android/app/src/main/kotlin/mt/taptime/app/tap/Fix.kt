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
     * Which providers the press asks, each ONCE, or none (the press then goes without a fix):
     *  * API 31+: the platform's fused provider when it is there and on -- it combines GPS and
     *    network itself and works with the coarse permission too (Android 12's "approximate");
     *  * otherwise, with the FINE permission: GPS **and** the network provider, both at once
     *    (review D-2, coordinator decision 2026-11-18): indoors GPS may not fix within the deadline,
     *    the network provider usually does; the first fresh reading wins and the other is cancelled
     *    (FixRace);
     *  * with the coarse permission only: the network provider.
     * [enabled] is LocationManager.isProviderEnabled; a provider the phone does not have is "off".
     */
    fun providers(sdk: Int, fine: Boolean, coarse: Boolean, enabled: (String) -> Boolean): List<String> {
        if (!fine && !coarse) return emptyList()
        if (sdk >= 31 && enabled(FUSED)) return listOf(FUSED)
        return listOfNotNull(GPS.takeIf { fine && enabled(it) }, NETWORK.takeIf { enabled(it) })
    }

    /** A reading taken before the request (a cache): refused by [accept], answered by one fresh ask (FixRace). */
    fun isStale(fixNanos: Long, pressNanos: Long): Boolean = fixNanos < pressNanos

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
        if (isStale(fixNanos, pressNanos)) return null
        if (!lat.isFinite() || !lng.isFinite() || lat < -90 || lat > 90 || lng < -180 || lng > 180) return null
        return Fix(lat, lng, mock)
    }
}

/**
 * THE ONE READING'S RACE, pure (FixRulesTest drives it; TapLocation.kt carries out its asks). Every ask
 * is made at the press, at most once per provider plus ONE fresh ask in all, and the whole race ends
 * at the first accepted reading, when every ask has answered empty, or at the deadline (TapLocation's,
 * FixDeadline.kt) -- whichever comes first. Nothing repeats: a bounded, finite set of one-shot asks.
 *
 *  * [start]: the first ask of every provider (FixRules.providers).
 *  * [answered]: an accepted reading ends the race with it (the other asks are cancelled); a
 *    REFUSED CACHE (FixRules.isStale -- API 30+'s getCurrentLocation may answer with a cached
 *    reading, which tap.js's maximumAge 0 would never use) is answered by ONE fresh ask of the same
 *    provider, within what is left of the deadline (review O-1, coordinator decision 2026-11-18);
 *    any other empty answer waits for the rest.
 */
class FixRace(private val providers: List<String>) {
    sealed interface Next {
        /** The race is over: send [fix] (null: no reading), cancel what is still asked. */
        data class Done(val fix: Fix?) : Next
        /** Ask [provider] once more for a FRESH reading. */
        data class AskFresh(val provider: String) : Next
        /** Other asks are still out. */
        data object Wait : Next
    }

    private val out = providers.toMutableSet()
    private var freshAsked = false
    var over = providers.isEmpty()
        private set

    fun start(): List<String> = providers

    /** One ask of [provider] answered: [fix] accepted (or null), [stale] if a cache was refused. */
    fun answered(provider: String, fix: Fix?, stale: Boolean): Next {
        if (over) return Next.Wait
        out.remove(provider)
        if (fix != null) return done(fix)
        if (stale && !freshAsked) {
            freshAsked = true
            out.add(provider)
            return Next.AskFresh(provider)
        }
        return if (out.isEmpty()) done(null) else Next.Wait
    }

    /** The deadline: whatever is out is cancelled and the press goes without a fix. */
    fun expired(): Next = if (over) Next.Wait else done(null)

    private fun done(fix: Fix?): Next {
        over = true
        return Next.Done(fix)
    }
}
