package mt.taptime.app

import android.location.LocationManager
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
    fun theProviderFollowsTheApiLevelThePermissionAndWhatIsOn() {
        val all: (String) -> Boolean = { true }
        val none: (String) -> Boolean = { false }
        fun only(vararg on: String): (String) -> Boolean = { it in on }

        assertNull("no permission, no provider", FixRules.provider(36, fine = false, coarse = false, all))
        assertNull("everything off", FixRules.provider(36, fine = true, coarse = true, none))

        // API 31+: the platform's fused provider first, with either permission.
        assertEquals(FixRules.FUSED, FixRules.provider(31, fine = true, coarse = true, all))
        assertEquals(FixRules.FUSED, FixRules.provider(36, fine = false, coarse = true, all))
        assertEquals(FixRules.GPS, FixRules.provider(31, fine = true, coarse = true, only(FixRules.GPS, FixRules.NETWORK)))

        // API 26-30 have no platform fused provider: GPS with the fine permission, else network.
        for (sdk in 26..30) {
            assertEquals(FixRules.GPS, FixRules.provider(sdk, fine = true, coarse = true, all))
            assertEquals(FixRules.NETWORK, FixRules.provider(sdk, fine = true, coarse = true, only(FixRules.NETWORK, FixRules.FUSED)))
            assertEquals("GPS needs the fine permission", FixRules.NETWORK, FixRules.provider(sdk, fine = false, coarse = true, all))
            assertNull(FixRules.provider(sdk, fine = false, coarse = true, only(FixRules.GPS)))
        }
    }

    @Test
    fun aReadingTakenBeforeThePressIsRefused() {
        val press = 1_000_000_000L
        assertNull("a cached reading (tap.js: maximumAge 0)", FixRules.accept(35.9, 14.4, false, press - 1, press))
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
