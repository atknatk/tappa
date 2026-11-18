package mt.taptime.app.tap

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Build
import android.os.Bundle
import android.os.CancellationSignal
import android.os.Looper
import android.os.SystemClock

// THE TAP'S ONE POSITION READING -- and the only file of this app that asks the platform for a
// position (CLAUDE.md §4.2; card M16-13). Every ask goes through ONE function, ask(), which holds the
// app's only two location requests; LocationCallSiteTest pins them there and pins ask()'s callers.
// read() has exactly one caller, the press in MainActivity. A one-shot request repeated by a timer is
// tracking, and the red-line scan's R2 periodic net sees only a timer in THIS file -- so the deadline
// lives in FixDeadline.kt and fires once.
//
// WHAT IS ASKED (FixRules.providers, FixRace): each chosen provider ONCE at the press -- on API 31+
// the platform's fused provider; below it, with the fine permission, GPS and network side by side
// (review D-2) -- plus at most ONE fresh ask when an answer was a refused cache (review O-1). The first
// accepted reading wins and every other ask is cancelled; the deadline (8 s, tap.js's) cancels all.
//
//	first ask, API 30+    LocationManager.getCurrentLocation: one answer, cancellable -- but it may
//	                      answer with a recent cache, which FixRules refuses;
//	first ask, API 26-29  LocationManager.requestSingleUpdate (the only one-shot request those have);
//	the fresh ask         requestSingleUpdate on every level: a new one-shot registration is answered
//	                      by a new reading, where getCurrentLocation would hand back the same cache
//	                      within its own window (deprecated on 30+, not removed; to be measured on a
//	                      real phone, M16-14).
//
// The platform's own LocationManager, not Google Play services' Fused Location Provider (Q36: no
// Play services artefact). Nothing is stored: the reading goes to the callback and from there into
// one request body. Nothing is logged.
//
// Every failure -- no permission, location off, no provider, a SecurityException, the deadline --
// delivers null, and the press goes on without a fix (§5 rows 6-7: GPS is backup evidence, never a
// requirement; the record is written either way, §4.6). Exactly one delivery, on the main thread.
class TapLocation(private val context: Context) {

    /** An ask that is out: how to take it back. */
    private fun interface Cancel {
        fun cancel()
    }

    fun read(onDone: (Fix?) -> Unit) {
        val lm = context.getSystemService(LocationManager::class.java) ?: return onDone(null)
        val providers = FixRules.providers(Build.VERSION.SDK_INT, granted(Manifest.permission.ACCESS_FINE_LOCATION),
            granted(Manifest.permission.ACCESS_COARSE_LOCATION)) { name -> isOn(lm, name) }
        val race = FixRace(providers)
        if (race.over) return onDone(null)
        val asked = SystemClock.elapsedRealtimeNanos()
        val out = mutableListOf<Cancel>()
        var delivered = false
        lateinit var deadline: FixDeadline

        fun finish(fix: Fix?) {
            if (delivered) return
            delivered = true
            deadline.cancel()
            out.forEach { it.cancel() }
            out.clear()
            onDone(fix)
        }

        lateinit var onAnswer: (String, Location?) -> Unit
        fun next(n: FixRace.Next) {
            when (n) {
                is FixRace.Next.Done -> finish(n.fix)
                is FixRace.Next.AskFresh -> ask(lm, n.provider, fresh = true, onAnswer)?.let(out::add) ?: next(race.answered(n.provider, null, false))
                FixRace.Next.Wait -> Unit
            }
        }
        onAnswer = { provider, loc ->
            if (!delivered) {
                val fix = loc?.let { FixRules.accept(it.latitude, it.longitude, isMock(it), it.elapsedRealtimeNanos, asked) }
                val stale = loc != null && fix == null && FixRules.isStale(loc.elapsedRealtimeNanos, asked)
                next(race.answered(provider, fix, stale))
            }
        }

        deadline = FixDeadline(FixRules.TIMEOUT_MILLIS) { next(race.expired()) }
        deadline.start()
        for (p in race.start()) {
            if (delivered) break
            ask(lm, p, fresh = false, onAnswer)?.let(out::add) ?: next(race.answered(p, null, false))
        }
    }

    // THE APP'S ONLY LOCATION REQUESTS: one one-shot ask of [provider]; null when the platform refused
    // it at once (no permission any more, the provider vanished). [onAnswer] gets the provider and the
    // reading, or null when the provider went off while asked.
    private fun ask(lm: LocationManager, provider: String, fresh: Boolean, onAnswer: (String, Location?) -> Unit): Cancel? = try {
        if (!fresh && Build.VERSION.SDK_INT >= 30) {
            val signal = CancellationSignal()
            lm.getCurrentLocation(provider, signal, context.mainExecutor) { loc -> onAnswer(provider, loc) }
            Cancel { signal.cancel() }
        } else {
            // The four callbacks are abstract on API 26-29 (defaults arrived in 30), so all four are
            // written; any answer ends this one ask.
            val listener = object : LocationListener {
                override fun onLocationChanged(loc: Location) = onAnswer(provider, loc)

                override fun onProviderDisabled(disabled: String) {
                    lm.removeUpdates(this)
                    onAnswer(provider, null)
                }

                override fun onProviderEnabled(enabled: String) = Unit

                @Deprecated("API 29 and lower call it; nothing to do")
                @Suppress("OVERRIDE_DEPRECATION")
                override fun onStatusChanged(name: String?, status: Int, extras: Bundle?) = Unit
            }
            @Suppress("DEPRECATION")
            lm.requestSingleUpdate(provider, listener, Looper.getMainLooper())
            Cancel { lm.removeUpdates(listener) }
        }
    } catch (_: SecurityException) {
        null // the permission was withdrawn between the check and the request
    } catch (_: IllegalArgumentException) {
        null // the provider vanished
    }

    private fun granted(permission: String) =
        context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED

    private fun isOn(lm: LocationManager, name: String): Boolean = try {
        lm.isProviderEnabled(name)
    } catch (_: IllegalArgumentException) {
        false // a provider this phone does not have (older platforms throw instead of answering false)
    }

    // The platform's mock flag for THIS reading (ADR 0045 §2.4): isMock on API 31+,
    // isFromMockProvider before. A client report: true tightens (base:mock-location-review),
    // false proves nothing -- so only true is ever sent (Bodies.tap).
    @Suppress("DEPRECATION")
    private fun isMock(loc: Location): Boolean =
        if (Build.VERSION.SDK_INT >= 31) loc.isMock else loc.isFromMockProvider
}
