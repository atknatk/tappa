package mt.taptime.app.tap

import android.os.Handler
import android.os.Looper

// The one reading's deadline: ONE delayed call on the main thread, never re-armed. It lives apart
// from TapLocation.kt on purpose (card M16-13, Tuzaklar): the red-line scan's R2 periodic net fails
// a file that holds both a one-shot location call and a timer, because a one-shot request repeated
// by a timer is tracking (CLAUDE.md §4.2). LocationCallSiteTest keeps location calls out of here.
class FixDeadline(private val millis: Long, private val onExpired: () -> Unit) {
    private val handler = Handler(Looper.getMainLooper())
    private val fire = Runnable { onExpired() }

    fun start() {
        handler.postDelayed(fire, millis)
    }

    fun cancel() {
        handler.removeCallbacks(fire)
    }
}
