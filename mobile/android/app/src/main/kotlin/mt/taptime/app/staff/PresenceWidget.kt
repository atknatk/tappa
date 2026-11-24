package mt.taptime.app.staff

import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.os.SystemClock
import android.view.View
import android.widget.RemoteViews
import mt.taptime.app.R
import mt.taptime.app.wire.Presence

// The home-screen widget (the user's request of 2026-11-24): "at work since 17:54" with a ticking
// chronometer, or "not clocked in". It makes NO request of its own -- the app publishes the state
// whenever it reads /me, and the widget ends the count at the business day's end (staleAt) by
// itself. What is kept on the phone is two instants and a clock: no name, no venue, no hours
// (My hours stay off the device, M16-33).

private const val PREFS = "presence_widget"

class PresenceWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
        draw(context, manager, ids)
    }

    companion object {
        /** Publishes /me's presence (null: not clocked in). [offsetMs] is server = phone + offset. */
        fun publish(context: Context, presence: Presence?, offsetMs: Long) {
            val e = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            if (presence == null) e.clear() else e.putLong("since", presence.inSinceMs - offsetMs)
                .putLong("until", presence.staleAtMs - offsetMs).putString("inLocal", presence.inSinceLocal)
            e.apply()
            redraw(context)
        }

        fun clear(context: Context) = publish(context, null, 0L)

        private fun redraw(context: Context) {
            val m = AppWidgetManager.getInstance(context) ?: return
            val ids = m.getAppWidgetIds(ComponentName(context, PresenceWidget::class.java))
            if (ids.isNotEmpty()) draw(context, m, ids)
        }

        private fun draw(context: Context, manager: AppWidgetManager, ids: IntArray) {
            val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            val since = prefs.getLong("since", 0L)
            val until = prefs.getLong("until", 0L)
            val now = System.currentTimeMillis()
            val atWork = since > 0L && now < until
            val v = RemoteViews(context.packageName, R.layout.widget_presence)
            v.setOnClickPendingIntent(R.id.widget_root, StaffChannels.openApp(context))
            if (atWork) {
                v.setImageViewResource(R.id.widget_dot, R.drawable.widget_dot_on)
                v.setTextViewText(R.id.widget_state, context.getString(R.string.today_at_work))
                v.setViewVisibility(R.id.widget_timer, View.VISIBLE)
                // The chronometer counts from an elapsed-realtime base: the same instant, moved off the wall clock.
                v.setChronometer(R.id.widget_timer, SystemClock.elapsedRealtime() - (now - since), null, true)
                v.setTextViewText(R.id.widget_line, context.getString(R.string.today_in_at, prefs.getString("inLocal", "").orEmpty()) +
                    " · " + context.getString(R.string.widget_tap_out))
            } else {
                v.setImageViewResource(R.id.widget_dot, R.drawable.widget_dot_off)
                v.setTextViewText(R.id.widget_state, context.getString(R.string.today_not_in))
                v.setChronometer(R.id.widget_timer, SystemClock.elapsedRealtime(), null, false)
                v.setViewVisibility(R.id.widget_timer, View.GONE)
                v.setTextViewText(R.id.widget_line, context.getString(R.string.today_tap_in))
            }
            manager.updateAppWidget(ids, v)
        }
    }
}
