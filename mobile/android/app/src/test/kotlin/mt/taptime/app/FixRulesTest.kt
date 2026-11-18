package mt.taptime.app

import android.location.LocationManager
import mt.taptime.app.tap.Fix
import mt.taptime.app.tap.FixRace
import mt.taptime.app.tap.FixRules
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The one reading's rules (tap/Fix.kt): which provider, which reading, how long. */
class FixRulesTest {
    @Test
    fun theProviderNamesAreThePlatforms() {
        // Java compile-time constants, inlined by the Kotlin compiler: no android.jar stub is called.
        assertEquals(LocationManager.FUSED_PROVIDER, FixRules.FUSED)
        assertEquals(LocationManager.GPS_PROVIDER, FixRules.GPS)
        assertEquals(LocationManager.NETWORK_PROVIDER, FixRules.NETWORK)
    }

    @Test
    fun theProvidersFollowTheApiLevelThePermissionAndWhatIsOn() {
        val all: (String) -> Boolean = { true }
        val none: (String) -> Boolean = { false }
        fun only(vararg on: String): (String) -> Boolean = { it in on }

        assertEquals("no permission, no provider", emptyList<String>(), FixRules.providers(36, fine = false, coarse = false, all))
        assertEquals("everything off", emptyList<String>(), FixRules.providers(36, fine = true, coarse = true, none))

        // API 31+: the platform's fused provider alone, with either permission.
        assertEquals(listOf(FixRules.FUSED), FixRules.providers(31, fine = true, coarse = true, all))
        assertEquals(listOf(FixRules.FUSED), FixRules.providers(36, fine = false, coarse = true, all))
        assertEquals(listOf(FixRules.GPS, FixRules.NETWORK), FixRules.providers(31, fine = true, coarse = true, only(FixRules.GPS, FixRules.NETWORK)))

        // API 26-30 (review D-2): with the fine permission GPS AND network, side by side.
        for (sdk in 26..30) {
            assertEquals(listOf(FixRules.GPS, FixRules.NETWORK), FixRules.providers(sdk, fine = true, coarse = true, all))
            assertEquals(listOf(FixRules.NETWORK), FixRules.providers(sdk, fine = true, coarse = true, only(FixRules.NETWORK, FixRules.FUSED)))
            assertEquals(listOf(FixRules.GPS), FixRules.providers(sdk, fine = true, coarse = false, only(FixRules.GPS)))
            assertEquals("GPS needs the fine permission", listOf(FixRules.NETWORK), FixRules.providers(sdk, fine = false, coarse = true, all))
            assertEquals(emptyList<String>(), FixRules.providers(sdk, fine = false, coarse = true, only(FixRules.GPS)))
        }
    }

    private val fix = Fix(35.9, 14.4, mock = false)

    // D-2: two providers asked at once; the first accepted reading wins and the race is over.
    @Test
    fun theFirstFreshReadingWinsAndTheOtherAskIsDropped() {
        val race = FixRace(listOf(FixRules.GPS, FixRules.NETWORK))
        assertEquals(listOf(FixRules.GPS, FixRules.NETWORK), race.start())
        assertEquals(FixRace.Next.Done(fix), race.answered(FixRules.NETWORK, fix, stale = false))
        assertTrue(race.over)
        assertEquals("a late answer changes nothing", FixRace.Next.Wait, race.answered(FixRules.GPS, Fix(1.0, 2.0, false), stale = false))
        assertEquals(FixRace.Next.Wait, race.expired())
    }

    @Test
    fun anEmptyAnswerWaitsForTheOtherAskAndAllEmptyIsNoFix() {
        val race = FixRace(listOf(FixRules.GPS, FixRules.NETWORK))
        assertEquals(FixRace.Next.Wait, race.answered(FixRules.GPS, null, stale = false))
        assertEquals(FixRace.Next.Done(null), race.answered(FixRules.NETWORK, null, stale = false))
        assertTrue(FixRace(emptyList()).over)
    }

    // O-1: a refused cache is answered by ONE fresh ask of the same provider; a second refused cache
    // is not asked again -- the race is a finite set of one-shot asks, never a repetition.
    @Test
    fun aRefusedCacheGetsOneFreshAskAndNoMore() {
        val race = FixRace(listOf(FixRules.FUSED))
        assertEquals(FixRace.Next.AskFresh(FixRules.FUSED), race.answered(FixRules.FUSED, null, stale = true))
        assertEquals("one fresh ask in all", FixRace.Next.Done(null), race.answered(FixRules.FUSED, null, stale = true))

        val fresh = FixRace(listOf(FixRules.FUSED))
        fresh.answered(FixRules.FUSED, null, stale = true)
        assertEquals("the fresh reading is used", FixRace.Next.Done(fix), fresh.answered(FixRules.FUSED, fix, stale = false))

        val two = FixRace(listOf(FixRules.GPS, FixRules.NETWORK))
        assertEquals(FixRace.Next.AskFresh(FixRules.GPS), two.answered(FixRules.GPS, null, stale = true))
        assertEquals("the fresh ask is out: wait for it", FixRace.Next.Wait, two.answered(FixRules.NETWORK, null, stale = true))
        assertEquals(FixRace.Next.Done(null), two.answered(FixRules.GPS, null, stale = false))
    }

    @Test
    fun theDeadlineEndsTheRaceWithoutAFix() {
        val race = FixRace(listOf(FixRules.FUSED))
        race.answered(FixRules.FUSED, null, stale = true) // a fresh ask is out
        assertEquals(FixRace.Next.Done(null), race.expired())
        assertEquals("nothing after the deadline", FixRace.Next.Wait, race.answered(FixRules.FUSED, fix, stale = false))
    }

    @Test
    fun aReadingTakenBeforeThePressIsRefused() {
        val press = 1_000_000_000L
        assertNull("a cached reading (tap.js: maximumAge 0)", FixRules.accept(35.9, 14.4, false, press - 1, press))
        assertTrue(FixRules.isStale(press - 1, press))
        assertFalse(FixRules.isStale(press, press))
        val fresh = FixRules.accept(35.9, 14.4, false, press, press)
        assertNotNull(fresh)
        assertEquals(35.9, fresh!!.lat, 0.0)
        assertFalse(fresh.mock)
        assertTrue("the mock flag rides with its reading", FixRules.accept(35.9, 14.4, true, press + 5, press)!!.mock)
    }

    @Test
    fun aReadingOutOfRangeIsNoReading() {
        for ((lat, lng) in listOf(91.0 to 0.0, -91.0 to 0.0, 0.0 to 181.0, 0.0 to -181.0, Double.NaN to 0.0, 0.0 to Double.POSITIVE_INFINITY)) {
            assertNull("$lat,$lng", FixRules.accept(lat, lng, false, 2, 1))
        }
        assertNotNull(FixRules.accept(-90.0, 180.0, false, 2, 1))
    }

    @Test
    fun thePressWaitsAsLongAsTheBrowserDoes() {
        // web/static/js/tap.js: var FIX_TIMEOUT_MS = 8000;
        val js = serverFile("taptime.js.tap").readText()
        val ms = Regex("""FIX_TIMEOUT_MS\s*=\s*(\d+)""").find(js)?.groupValues?.get(1)?.toLong()
            ?: throw AssertionError("no FIX_TIMEOUT_MS in tap.js")
        assertEquals(ms, FixRules.TIMEOUT_MILLIS)
    }
}
