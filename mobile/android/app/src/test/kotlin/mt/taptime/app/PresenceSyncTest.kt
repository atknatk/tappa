package mt.taptime.app

import mt.taptime.app.staff.PresenceSync
import mt.taptime.app.staff.ReminderPort
import mt.taptime.app.staff.StaffSwitches
import mt.taptime.app.staff.TimerPort
import mt.taptime.app.wire.Presence
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// [G46-b], [G47-b]: PresenceSync over fake ports -- one reminder with a fixed identity, the timer
// from the server's inSince to showUntil, both off with their switches, both cleared on a 401, and a
// swiped timer not shown again for the same check-in.
class PresenceSyncTest {
    private class Ports : ReminderPort, TimerPort, StaffSwitches {
        override var remind = true
        override var timer = true
        var reminderAt: Long? = null
        var reminderWords: Pair<String, String?>? = null
        var timerSpan: Pair<Long, Long>? = null
        var reminderSets = 0
        override fun schedule(atLocalMs: Long, inSinceLocal: String, shiftEndsAtLocal: String?, lastLocalMs: Long) {
            reminderAt = atLocalMs; reminderWords = inSinceLocal to shiftEndsAtLocal; reminderSets++
        }
        override fun cancel() { reminderAt = null }
        override fun show(sinceLocalMs: Long, untilLocalMs: Long, shiftEndsAtLocal: String?, key: Long) { timerSpan = sinceLocalMs to untilLocalMs }
        fun cancelTimer() { timerSpan = null }
    }

    private fun sync(p: Ports, dismissed: Long? = null): PresenceSync {
        val timer = object : TimerPort {
            override fun show(sinceLocalMs: Long, untilLocalMs: Long, shiftEndsAtLocal: String?, key: Long) = p.show(sinceLocalMs, untilLocalMs, shiftEndsAtLocal, key)
            override fun cancel() = p.cancelTimer()
        }
        return PresenceSync(p, timer, p) { dismissed }
    }

    private val at = Presence(inSinceMs = 1_000_000, inSinceLocal = "09:00", shiftEndsAtMs = null, shiftEndsAtLocal = null,
        remindAtMs = 5_000_000, showUntilMs = 7_000_000, staleAtMs = 9_000_000)

    @Test
    fun aFutureReminderIsScheduledOnceOnThePhonesClock() {
        val p = Ports()
        val s = sync(p)
        // The server is 500 ms ahead of the phone: the phone's instants are the server's minus that.
        s.apply(at, offsetMs = 500, nowLocalMs = 2_000_000)
        assertEquals(4_999_500L, p.reminderAt)
        assertEquals("09:00" to null, p.reminderWords)
        assertEquals(999_500L to 6_999_500L, p.timerSpan)
        s.apply(at, offsetMs = 500, nowLocalMs = 2_000_000)
        assertEquals("a second sync moves the one reminder", 2, p.reminderSets)
    }

    @Test
    fun aPastReminderOrNoPresenceSchedulesNothing() {
        val p = Ports()
        val s = sync(p)
        s.apply(at, 0, nowLocalMs = 6_000_000)
        assertNull("the reminder's moment passed", p.reminderAt)
        s.apply(null, 0, nowLocalMs = 2_000_000)
        assertNull(p.reminderAt)
        assertNull("no presence ends the timer at once", p.timerSpan)
    }

    @Test
    fun theSwitchesTurnEachOff() {
        val p = Ports().apply { remind = false; timer = false }
        sync(p).apply(at, 0, 2_000_000)
        assertNull(p.reminderAt)
        assertNull(p.timerSpan)
    }

    @Test
    fun aSwipedTimerStaysAwayForThatCheckIn() {
        val p = Ports()
        sync(p, dismissed = at.inSinceMs).apply(at, 0, 2_000_000)
        assertNull(p.timerSpan)
        sync(p, dismissed = 42).apply(at, 0, 2_000_000)
        assertEquals(1_000_000L to 7_000_000L, p.timerSpan)
    }

    @Test
    fun aSignOutClearsBoth() {
        val p = Ports()
        val s = sync(p)
        s.apply(at, 0, 2_000_000)
        s.clear()
        assertNull(p.reminderAt)
        assertNull(p.timerSpan)
    }
}
