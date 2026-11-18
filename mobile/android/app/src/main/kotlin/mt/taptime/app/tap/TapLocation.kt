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
// position (CLAUDE.md §4.2; card M16-13). LocationCallSiteTest pins it: the two one-shot calls
// below are the only location requests under src/main, and this file's read() has exactly one
// caller, the press in MainActivity. A one-shot request repeated by a timer is tracking, and the
// red-line scan's R2 periodic net sees only a timer in THIS file -- so the deadline lives in
// FixDeadline.kt and fires once.
//
//	API 30+    LocationManager.getCurrentLocation: one answer, cancellable.
//	API 26-29  LocationManager.requestSingleUpdate (deprecated in 30, the only one-shot request
//	           those versions have), removed at the deadline.
//
// The platform's own LocationManager, not Google Play services' Fused Location Provider (Q36: no
// Play services artefact). Nothing is stored: the reading goes to the callback and from there into
// one request body. Nothing is logged.
//
// Every failure -- no permission, location off, no provider, a SecurityException, the deadline --
// delivers null, and the press goes on without a fix (§5 rows 6-7: GPS is backup evidence, never a
// requirement; the record is written either way, §4.6). Exactly one delivery, on the main thread.
class TapLocation(private val context: Context) {

    fun read(onDone: (Fix?) -> Unit) {
        var delivered = false
        fun deliver(fix: Fix?) {
            if (delivered) return
            delivered = true
            onDone(fix)
        }

        val lm = context.getSystemService(LocationManager::class.java) ?: return deliver(null)
        val provider = FixRules.provider(Build.VERSION.SDK_INT, granted(Manifest.permission.ACCESS_FINE_LOCATION),
            granted(Manifest.permission.ACCESS_COARSE_LOCATION)) { name -> isOn(lm, name) }
            ?: return deliver(null)
        val asked = SystemClock.elapsedRealtimeNanos()
        fun accept(loc: Location?) = deliver(loc?.let { FixRules.accept(it.latitude, it.longitude, isMock(it), it.elapsedRealtimeNanos, asked) })

        try {
            if (Build.VERSION.SDK_INT >= 30) {
                val cancel = CancellationSignal()
                val deadline = FixDeadline(FixRules.TIMEOUT_MILLIS) {
                    cancel.cancel()
                    deliver(null)
                }
                lm.getCurrentLocation(provider, cancel, context.mainExecutor) { loc ->
                    deadline.cancel()
                    accept(loc)
                }
                deadline.start()
            } else {
                // The four callbacks are abstract on API 26-29 (defaults arrived in 30), so all four
                // are written; any of them ends the one reading.
                lateinit var deadline: FixDeadline
                val listener = object : LocationListener {
                    override fun onLocationChanged(loc: Location) {
                        deadline.cancel()
                        accept(loc)
                    }

                    override fun onProviderDisabled(provider: String) {
                        deadline.cancel()
                        lm.removeUpdates(this)
                        deliver(null)
                    }

                    override fun onProviderEnabled(provider: String) = Unit

                    @Deprecated("API 29 and lower call it; nothing to do")
                    @Suppress("OVERRIDE_DEPRECATION")
                    override fun onStatusChanged(provider: String?, status: Int, extras: Bundle?) = Unit
                }
                deadline = FixDeadline(FixRules.TIMEOUT_MILLIS) {
                    lm.removeUpdates(listener)
                    deliver(null)
                }
                @Suppress("DEPRECATION")
                lm.requestSingleUpdate(provider, listener, Looper.getMainLooper())
                deadline.start()
            }
        } catch (_: SecurityException) {
            deliver(null) // the permission was withdrawn between the check and the request
        } catch (_: IllegalArgumentException) {
            deliver(null) // the provider vanished
        }
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
