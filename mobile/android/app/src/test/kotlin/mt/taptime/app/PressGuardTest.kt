package mt.taptime.app

import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.KeyBacking
import mt.taptime.app.tap.PressGuard
import mt.taptime.app.tap.TapFlow
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.TapStartReply
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * ONE PRESS PER TAP SCREEN (security review, 2026-11-18): two clicks delivered in one frame -- the
 * primary button and "Not right? …" -- carry the same captured screen; the live state decides and the
 * first one marks it busy. Driven the way MainActivity.press uses it: read the live screen, ask the
 * guard, set busy.
 */
class PressGuardTest {
    private fun ready() = TapFlow.Start.Ready(
        ActiveDevice("00000000-0000-4000-8000-000000000001", "alias", KeyBacking.TEE, "Maria Borg"),
        JdkSigner(), "tag=04A1&ctr=000011&cmac=0011223344556677",
        TapStartReply("Maria Borg", "St Julians", Brand.TAPTIME, "in", false, "ctx"),
    )

    /** MainActivity's live screen, reduced to what the guard reads. */
    private class Live(var ready: TapFlow.Start.Ready?, var busy: Boolean = false) {
        var posts = 0
        fun press(captured: TapFlow.Start.Ready) {
            if (!PressGuard.mayPress(ready, busy, captured)) return
            busy = true
            posts++
        }
    }

    @Test
    fun twoClicksInOneFrameSendOnePress() {
        val r = ready()
        val live = Live(r)
        // Both clicks were drawn from the same composition: both captured [r].
        live.press(r) // the primary button
        live.press(r) // "Not right? Clock out", same frame
        assertEquals(1, live.posts)
    }

    @Test
    fun aClickDrawnForAnotherScreenDoesNothing() {
        val old = ready()
        val live = Live(ready())
        live.press(old)
        assertEquals("a newer tap screen replaced the one the click was drawn for", 0, live.posts)
        live.ready = null
        live.press(old)
        assertEquals("no tap screen at all", 0, live.posts)
    }

    @Test
    fun theGuardIsIdentityNotEquality() {
        val a = ready()
        val b = a.copy()
        assertTrue(a == b)
        assertFalse("an equal but different screen is another screen", PressGuard.mayPress(a, false, b))
        assertTrue(PressGuard.mayPress(a, false, a))
        assertFalse(PressGuard.mayPress(a, true, a))
    }
}
