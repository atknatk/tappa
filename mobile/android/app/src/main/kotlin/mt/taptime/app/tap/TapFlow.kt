package mt.taptime.app.tap

import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.DeviceKeys
import mt.taptime.app.activation.DeviceState
import mt.taptime.app.activation.PlaqueRead
import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.AppErrorCode
import mt.taptime.app.wire.Bodies
import mt.taptime.app.wire.Direction
import mt.taptime.app.wire.Outcome
import mt.taptime.app.wire.Signer
import mt.taptime.app.wire.TapReply
import mt.taptime.app.wire.TapStartReply

/**
 * The tap screens' closed set of problems. Each is a fixed text (strings.xml): the browser's tap
 * problem texts (internal/handler/tap.go) where it has one, the app's own where it has none.
 */
enum class TapProblem {
    /** tapProblemBadURL: the plaque URL is not this server's, or the server could not parse it. */
    BAD_LINK,
    /** tapProblemUnknownTag. */
    UNKNOWN_PLAQUE,
    /** tapProblemServer. */
    SERVER,
    /** tapProblemTooMany. */
    TOO_MANY,
    /** tapProblemStale: the signed context expired before the press. */
    STALE,
    /** tapProblemForeignTenant. */
    FOREIGN,
    /** APP-ONLY: no answer to tap/start. Nothing was recorded (tap/start writes nothing). */
    UNREACHABLE,
    /** APP-ONLY: no readable answer to the PRESS. The record may or may not exist: tap again. */
    LOST,
    /** APP-ONLY (iOS's keyUnreadable): the Keystore did not hand out the device key. */
    KEY_UNREADABLE,
    /** Two 401s (AppClient's clock-corrected retry): the browser's "This phone was signed out". */
    SIGNED_OUT,
    /** APP-ONLY: no device on this phone (or its key is gone for good): iOS's notSetUp. */
    NOT_SET_UP,
    /**
     * APP-ONLY (M16-16 hand-over (4), iOS's clock): a 401 while the last Date header was more than a
     * day off this phone's clock (ServerClock.lastAnswerWasFarOff) -- the phone's clock, not a
     * signed-out device.
     */
    CLOCK,
}

/** What a problem screen's "Try again" does. */
enum class Retry {
    /** No "Try again". */
    NONE,
    /** tap/start again with the same URL: safe, nothing reached the record (start writes nothing). */
    RESTART,
    /**
     * Back to the app's home (the launch check): resends NOTHING. The press's answer was lost and
     * the record may exist, so the same URL is never sent again (coordinator decision 2026-11-18,
     * the iOS twin's offlineUnknown).
     */
    HOME,
}

/**
 * The buttons of a problem screen, the iOS twin's TapProblemScreen / NotSetUpScreen, as a pure
 * function. "Open in browser" only where the plaque URL is certainly unspent AND the app cannot go
 * on -- not set up, signed out, no answer BEFORE the press (coordinator decisions 2026-11-18): after
 * a press whose answer was lost the same URL could record a reject and start the debounce.
 */
data class Buttons(val tryAgain: Retry, val browser: Boolean, val startAgain: Boolean, val setUp: Boolean) {
    companion object {
        fun of(problem: TapProblem, retry: Retry): Buttons = when (problem) {
            TapProblem.NOT_SET_UP -> Buttons(Retry.NONE, browser = true, startAgain = false, setUp = true)
            TapProblem.SIGNED_OUT -> Buttons(Retry.NONE, browser = true, startAgain = true, setUp = false)
            TapProblem.UNREACHABLE -> Buttons(retry, browser = true, startAgain = false, setUp = false)
            else -> Buttons(retry, browser = false, startAgain = false, setUp = false)
        }
    }
}

/** A key the Keystore failed to load or to sign with: the screen is "couldn't open this phone's key". */
class KeyUnreadable(cause: Throwable) : Exception("device key unreadable", cause)

// THE APP'S TAP (card M16-13), GET /t + POST /api/checkin's twin over /app/v1:
//
//	start   the plaque's URL (the NFC dispatch, or an App Link for a QR / a link): this server's
//	        <base>/t only (PlaqueRead); its query goes to POST /app/v1/tap/start, a SIGNED READ --
//	        no nonce, no counter advanced, nothing written. The answer is the tap screen: the
//	        person's name, the tapped venue, the brand, the predicted direction, the signed context.
//	press   the one button (or "Not right? …"): POST /app/v1/tap, nonce-signed, with the context,
//	        the pressed direction, ONE fix read at the press (TapLocation, §4.2) and the mock flag
//	        when true. The server runs the same checkin.Service.Record as the browser; the app
//	        decides nothing (ADR 0045 §2.1).
//
// NO QUEUE (ADR 0045 §2.8, coordinator decision 2026-11-18). A press with no readable answer is
// LOST: the screen says "tap the plaque again". The next touch is a fresh counter; the plaque's
// counter gap (A1's ctrGap) counts the lost one. Retrying the SAME context is never offered: if the
// first press did reach the record, the same counter again is a recorded sun-invalid reject.
class TapFlow(
    private val client: AppClient,
    private val keys: DeviceKeys,
    private val state: DeviceState,
    private val baseUrl: String,
) {
    sealed interface Start {
        /** The tap screen. [sun] is kept for "Try again" and for nothing else. */
        data class Ready(val device: ActiveDevice, val signer: Signer, val sun: String, val facts: TapStartReply) : Start {
            // The SUN query carries the chip's CMAC and counter: never in a printed value (§7).
            override fun toString() = "Ready(device=${device.deviceId}, sun=<redacted>, facts=$facts)"
        }
        data class Problem(val problem: TapProblem, val retry: Retry) : Start
    }

    sealed interface Press {
        data class Recorded(val reply: TapReply) : Press
        data class Problem(val problem: TapProblem, val retry: Retry) : Press
    }

    // A 401 that survived the clock-corrected retry: signed out -- unless the last answer's Date was
    // more than a day off this phone's clock, which is the phone's clock (iOS's clock screen). A 401
    // records nothing, so the URL is unspent either way: "Try again" may start over.
    private fun refusedSignature(): TapProblem =
        if (client.clock.lastAnswerWasFarOff) TapProblem.CLOCK else TapProblem.SIGNED_OUT

    private fun retryOf(p: TapProblem) = if (p == TapProblem.CLOCK) Retry.RESTART else Retry.NONE

    fun start(url: String): Start {
        val sun = PlaqueRead.sunQuery(url, baseUrl) ?: return Start.Problem(TapProblem.BAD_LINK, Retry.NONE)
        val device = state.active() ?: return Start.Problem(TapProblem.NOT_SET_UP, Retry.NONE)
        val signer = try {
            loadTwice(device.keyAlias)
        } catch (_: KeyUnreadable) {
            return Start.Problem(TapProblem.KEY_UNREADABLE, Retry.RESTART)
        }
        if (signer == null) {
            // The Keystore entry is gone for good: nothing can ever sign for this record
            // (ActivationFlow.home drops it the same way).
            state.clear()
            return Start.Problem(TapProblem.NOT_SET_UP, Retry.NONE)
        }
        val r = try {
            client.tapStart(device.deviceId, Guarded(signer), sun)
        } catch (_: KeyUnreadable) {
            return Start.Problem(TapProblem.KEY_UNREADABLE, Retry.RESTART)
        }
        return when (r) {
            is Outcome.Ok -> {
                val name = r.value.employeeName
                if (name.isNotEmpty() && name != device.employeeName) state.save(device.copy(employeeName = name))
                Start.Ready(device, signer, sun, r.value)
            }
            Outcome.Unauthenticated -> refusedSignature().let { Start.Problem(it, retryOf(it)) }
            is Outcome.Failed -> Start.Problem(TapProblem.UNREACHABLE, Retry.RESTART)
            // GET /t's answers (internal/handler/tap.go): an unusable URL is not retried, an unknown
            // uid is not, the rate limit deliberately is not, a server failure is.
            is Outcome.Refused -> when (r.code) {
                AppErrorCode.BAD_REQUEST -> Start.Problem(TapProblem.BAD_LINK, Retry.NONE)
                AppErrorCode.UNKNOWN_PLAQUE -> Start.Problem(TapProblem.UNKNOWN_PLAQUE, Retry.NONE)
                AppErrorCode.RATE_LIMITED -> Start.Problem(TapProblem.TOO_MANY, Retry.NONE)
                else -> Start.Problem(TapProblem.SERVER, Retry.RESTART)
            }
        }
    }

    /**
     * The press. [want] is the pressed button's direction ("in" / "out"), null for the
     * direction-neutral "Tap"; [fix] the one reading taken at the press, or null.
     */
    fun press(ready: Start.Ready, want: String?, fix: Fix?): Press {
        val body = Bodies.tap(ready.facts.context, want?.let(Direction::of)?.takeIf { it.isNotEmpty() },
            fix?.lat, fix?.lng, fix?.mock == true)
        val r = try {
            client.tap(ready.device.deviceId, Guarded(ready.signer), body)
        } catch (_: KeyUnreadable) {
            // Signing failed before POST /app/v1/tap went out: nothing reached the record.
            return Press.Problem(TapProblem.KEY_UNREADABLE, Retry.RESTART)
        }
        return when (r) {
            is Outcome.Ok -> Press.Recorded(r.value)
            // A refused signature records nothing: the URL is unspent (signed out keeps its exits).
            Outcome.Unauthenticated -> refusedSignature().let { Press.Problem(it, retryOf(it)) }
            // The answer is lost: "Try again" goes home and resends nothing (coordinator decision).
            is Outcome.Failed -> Press.Problem(TapProblem.LOST, Retry.HOME)
            // POST /api/checkin's answers: none offers a retry (internal/handler/tap.go,
            // renderRetryableProblem's table) -- a fresh context comes from touching the plaque.
            is Outcome.Refused -> Press.Problem(
                when (r.code) {
                    AppErrorCode.STALE_CONTEXT -> TapProblem.STALE
                    AppErrorCode.FOREIGN_TENANT -> TapProblem.FOREIGN
                    AppErrorCode.UNKNOWN_PLAQUE -> TapProblem.UNKNOWN_PLAQUE
                    AppErrorCode.RATE_LIMITED -> TapProblem.TOO_MANY
                    else -> TapProblem.SERVER
                },
                Retry.NONE,
            )
        }
    }

    // ONE retry before the problem screen, never a deletion (M16-12 hand-over (3), the iOS twin's
    // rule): a Keystore that fails once usually answers the next time, and a deleted key never would.
    private fun loadTwice(alias: String): Signer? {
        repeat(2) { attempt ->
            try {
                return keys.load(alias)
            } catch (e: Exception) {
                if (attempt == 1) throw KeyUnreadable(e)
            }
        }
        return null
    }

    /** A signer whose failures are the Keystore's: they surface as [KeyUnreadable], never as a crash. */
    private class Guarded(private val inner: Signer) : Signer {
        override val publicPoint: ByteArray
            get() = try {
                inner.publicPoint
            } catch (e: Exception) {
                throw KeyUnreadable(e)
            }

        override fun sign(message: ByteArray): ByteArray = try {
            inner.sign(message)
        } catch (e: Exception) {
            throw KeyUnreadable(e)
        }
    }
}
