package mt.taptime.app.staff

import mt.taptime.app.wire.Presence

// THE ONE SYNC of the "Still at work?" reminder (M16-46) and the lock-screen timer (M16-47), from
// /me's presence and the person's two switches. The phone computes no direction, no zone and no
// shift: every instant is the server's, corrected by the server-clock offset, and every clock in a
// word is the server's "HH:MM".
//
// NO PUSH, NO LOCATION, NO BACKGROUND WORK (§4.2): a single inexact alarm the platform fires (a
// reboot drops it; the next open sets it again) and an ongoing notification whose chronometer the
// system draws. Nothing runs between them.
//
// A FAILED /me LEAVES BOTH AS THEY ARE: the last answer the server gave stands (Faz G rule).

/** The reminder's port: one pending alarm with a fixed identity. */
interface ReminderPort {
    /** Sets (or moves) THE reminder to [atLocalMs] on the phone's clock, with its words' clocks. */
    /**
     * [lastLocalMs] is the latest moment a snoozed reminder may fire: the server's staleAt less
     * the reminder's 30-minute clip (M16-45) -- past it the next tap is a new check-in, so "clock
     * out?" would be false.
     */
    fun schedule(atLocalMs: Long, inSinceLocal: String, shiftEndsAtLocal: String?, lastLocalMs: Long)
    fun cancel()
}

/** The lock-screen timer's port: one ongoing notification. */
interface TimerPort {
    /** [key] is the check-in's server instant: a swipe reports it back (dismissed). */
    fun show(sinceLocalMs: Long, untilLocalMs: Long, shiftEndsAtLocal: String?, key: Long)
    fun cancel()
}

/** The person's two switches, as flags on the phone (booleans, never a time or a name). */
interface StaffSwitches {
    val remind: Boolean
    val timer: Boolean
}

/** The reminder's clip before staleAt (M16-45's remindBeforeStale), for a snoozed reminder too. */
const val SNOOZE_CLIP_MS = 30L * 60 * 1000

class PresenceSync(
    private val reminder: ReminderPort,
    private val timer: TimerPort,
    private val switches: StaffSwitches,
    // The check-in whose timer the person swiped away (Q53 (4) (a)): not shown again for the same
    // check-in. Memory only -- never written to disk.
    private val dismissed: () -> Long? = { null },
) {
    /**
     * Applies /me's presence. [offsetMs] is ServerClock's (server = local + offset); [nowLocalMs] is
     * the phone's clock.
     */
    fun apply(presence: Presence?, offsetMs: Long, nowLocalMs: Long) {
        val nowServer = nowLocalMs + offsetMs
        val remindAt = presence?.remindAtMs
        if (presence != null && switches.remind && remindAt != null && remindAt > nowServer) {
            reminder.schedule(remindAt - offsetMs, presence.inSinceLocal, presence.shiftEndsAtLocal, presence.staleAtMs - offsetMs - SNOOZE_CLIP_MS)
        } else {
            reminder.cancel()
        }
        if (presence != null && switches.timer && presence.showUntilMs > nowServer && dismissed() != presence.inSinceMs) {
            timer.show(presence.inSinceMs - offsetMs, presence.showUntilMs - offsetMs, presence.shiftEndsAtLocal, presence.inSinceMs)
        } else {
            timer.cancel()
        }
    }

    /** Sign-out, a removed phone, a 401: nothing of the person stays scheduled. */
    fun clear() {
        reminder.cancel()
        timer.cancel()
    }
}
