package mt.taptime.app.staff

import android.app.AlarmManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import mt.taptime.app.MainActivity
import mt.taptime.app.R

// The platform half of PresenceSync (M16-46, M16-47): the platform's AlarmManager and
// NotificationManager -- no androidx NotificationCompat, no WorkManager, no foreground service, no
// boot receiver (CLAUDE.md §1, §4.2). minSdk 26 has every API used here.
//
// WHAT THE LOCK SCREEN SHOWS: "Clocked in", the system's chronometer and, only when the owner marked
// shifts real, "Shift ends 17:00". No name, no business, no venue (Q53 (2) (a)).

private const val CHANNEL_REMINDERS = "clock_out_reminders"
private const val CHANNEL_TIMER = "at_work_timer"
private const val ID_REMINDER = 1
private const val ID_TIMER = 2
private const val ACTION_REMIND = "mt.taptime.app.action.REMIND"
private const val ACTION_TIMER_DISMISSED = "mt.taptime.app.action.TIMER_DISMISSED"
private const val EXTRA_IN = "in"
private const val EXTRA_SHIFT_END = "shiftEnd"
private const val EXTRA_LAST = "last"
private const val ACTION_SNOOZE = "mt.taptime.app.action.SNOOZE"
private const val SNOOZE_MS = 60L * 60 * 1000

/** The person's two switches, in the app's private preferences: two booleans. */
class PrefsStaffSwitches(context: Context) : StaffSwitches {
    private val prefs: SharedPreferences = context.getSharedPreferences("staff_switches", Context.MODE_PRIVATE)
    override var remind: Boolean
        get() = prefs.getBoolean("remind", false)
        set(v) = prefs.edit().putBoolean("remind", v).apply()
    override var timer: Boolean
        get() = prefs.getBoolean("timer", false)
        set(v) = prefs.edit().putBoolean("timer", v).apply()
}

object StaffChannels {
    fun ensure(context: Context) {
        val nm = context.getSystemService(NotificationManager::class.java) ?: return
        nm.createNotificationChannel(NotificationChannel(CHANNEL_REMINDERS,
            context.getString(R.string.channel_reminders), NotificationManager.IMPORTANCE_DEFAULT))
        nm.createNotificationChannel(NotificationChannel(CHANNEL_TIMER,
            context.getString(R.string.channel_timer), NotificationManager.IMPORTANCE_LOW).apply { setSound(null, null) })
    }

    /** Opens the app's home screen; reads no plaque. */
    fun openApp(context: Context): PendingIntent = PendingIntent.getActivity(context, 0,
        Intent(context, MainActivity::class.java).setAction(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
}

/** The reminder: ONE inexact alarm (no exact-alarm permission), a fixed request code. */
class AlarmReminder(private val context: Context) : ReminderPort {
    private val alarms = context.getSystemService(AlarmManager::class.java)

    private fun pending(inLocal: String?, shiftEnd: String?, lastMs: Long, flags: Int): PendingIntent? =
        PendingIntent.getBroadcast(context, ID_REMINDER,
            Intent(context, StaffReceiver::class.java).setAction(ACTION_REMIND)
                .apply {
                    if (inLocal != null) putExtra(EXTRA_IN, inLocal)
                    if (shiftEnd != null) putExtra(EXTRA_SHIFT_END, shiftEnd)
                    putExtra(EXTRA_LAST, lastMs)
                },
            flags or PendingIntent.FLAG_IMMUTABLE)

    override fun schedule(atLocalMs: Long, inSinceLocal: String, shiftEndsAtLocal: String?, lastLocalMs: Long) {
        val p = pending(inSinceLocal, shiftEndsAtLocal, lastLocalMs, PendingIntent.FLAG_UPDATE_CURRENT) ?: return
        // Inexact: Android 12+ may deliver it up to about an hour late, later under Doze (M16-46's
        // named cost); the words carry the check-in's clock, not "now".
        alarms?.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, atLocalMs, p)
    }

    override fun cancel() {
        pending(null, null, 0L, PendingIntent.FLAG_NO_CREATE)?.let { alarms?.cancel(it); it.cancel() }
        context.getSystemService(NotificationManager::class.java)?.cancel(ID_REMINDER)
    }
}

/** The lock-screen timer: an ongoing notification whose chronometer the system draws. */
class OngoingTimer(private val context: Context) : TimerPort {
    override fun show(sinceLocalMs: Long, untilLocalMs: Long, shiftEndsAtLocal: String?, key: Long) {
        val nm = context.getSystemService(NotificationManager::class.java) ?: return
        StaffChannels.ensure(context)
        val dismissed = PendingIntent.getBroadcast(context, ID_TIMER,
            Intent(context, StaffReceiver::class.java).setAction(ACTION_TIMER_DISMISSED).putExtra("key", key),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val b = Notification.Builder(context, CHANNEL_TIMER)
            .setSmallIcon(R.drawable.ic_stat_taptime)
            .setContentTitle(context.getString(R.string.timer_title))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(true)
            .setUsesChronometer(true)
            .setWhen(sinceLocalMs)
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .setContentIntent(StaffChannels.openApp(context))
            .setDeleteIntent(dismissed)
            .setTimeoutAfter((untilLocalMs - System.currentTimeMillis()).coerceAtLeast(1L))
        if (shiftEndsAtLocal != null) b.setContentText(context.getString(R.string.timer_shift_ends, shiftEndsAtLocal))
        nm.notify(ID_TIMER, b.build())
    }

    override fun cancel() {
        context.getSystemService(NotificationManager::class.java)?.cancel(ID_TIMER)
    }
}

/**
 * The app's one receiver, not exported: the alarm's "Still at work?" and the timer's swipe. It is
 * reached only by this app's own PendingIntents.
 */
class StaffReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            ACTION_REMIND -> {
                val inLocal = intent.getStringExtra(EXTRA_IN) ?: return
                val shiftEnd = intent.getStringExtra(EXTRA_SHIFT_END)
                val last = intent.getLongExtra(EXTRA_LAST, 0L)
                val nm = context.getSystemService(NotificationManager::class.java) ?: return
                StaffChannels.ensure(context)
                val body = if (shiftEnd != null) context.getString(R.string.remind_body_shift, shiftEnd)
                else context.getString(R.string.remind_body, inLocal)
                val b = Notification.Builder(context, CHANNEL_REMINDERS)
                    .setSmallIcon(R.drawable.ic_stat_taptime)
                    .setContentTitle(context.getString(R.string.remind_title))
                    .setContentText(body)
                    .setStyle(Notification.BigTextStyle().bigText(body))
                    .setAutoCancel(true)
                    .setContentIntent(StaffChannels.openApp(context))
                    .addAction(Notification.Action.Builder(null, context.getString(R.string.remind_open), StaffChannels.openApp(context)).build())
                // "Remind me in an hour" only while an hour from now is still before the business
                // day's end: after it, a tap is a new check-in and the question would be false.
                if (System.currentTimeMillis() + SNOOZE_MS <= last) {
                    val snooze = PendingIntent.getBroadcast(context, ID_REMINDER + 10,
                        Intent(context, StaffReceiver::class.java).setAction(ACTION_SNOOZE)
                            .putExtra(EXTRA_IN, inLocal).putExtra(EXTRA_LAST, last)
                            .apply { if (shiftEnd != null) putExtra(EXTRA_SHIFT_END, shiftEnd) },
                        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
                    b.addAction(Notification.Action.Builder(null, context.getString(R.string.remind_snooze), snooze).build())
                }
                nm.notify(ID_REMINDER, b.build())
            }
            ACTION_SNOOZE -> {
                val inLocal = intent.getStringExtra(EXTRA_IN) ?: return
                val last = intent.getLongExtra(EXTRA_LAST, 0L)
                context.getSystemService(NotificationManager::class.java)?.cancel(ID_REMINDER)
                val at = System.currentTimeMillis() + SNOOZE_MS
                if (at <= last) AlarmReminder(context).schedule(at, inLocal, intent.getStringExtra(EXTRA_SHIFT_END), last)
            }
            ACTION_TIMER_DISMISSED -> TimerDismissals.key = intent.getLongExtra("key", 0L)
        }
    }
}

/** The swiped timer's check-in (its server instant), memory only: PresenceSync's `dismissed`. */
object TimerDismissals {
    @Volatile
    var key: Long? = null
}
