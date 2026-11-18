package mt.taptime.app.activation

import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.AppErrorCode
import mt.taptime.app.wire.BindReply
import mt.taptime.app.wire.LookupReply
import mt.taptime.app.wire.Outcome

/** The screens' closed set of problems; each one is a fixed text (strings.xml), never a server value. */
enum class Problem { BAD_LINK, TOO_MANY, SERVER, NETWORK, TAP_FAILED, NEEDS_TOUCH, NOT_READY, SIGNED_OUT }

/** The bind the tap step holds: the code, the fresh key and the server's challenge. Memory only. */
class PendingActivation(val code: String, val facts: LookupReply, val key: DeviceKey, val bound: BindReply)

// THE APP'S ACTIVATION, ADR 0026's two steps for a device key (ADR 0045 §2.3; the server side is
// internal/handler/appactivate.go):
//
//	code       the person pastes the invitation link (Q40: the link opens the browser, not
//	           the app); InviteCode reads the code out of it
//	lookup     the consent screen's facts -- employer, person, venue, Wi-Fi, brand -- spends nothing
//	bind       "Agree and continue": a NEW Keystore key, its public point and the consent. The
//	           server records the consent bound to THIS key and a challenge it drew; nothing is
//	           spent and no device exists yet
//	tap        the first genuine NFC read of an employer's plaque: its SUN query goes to
//	           complete, signed by the bound key over the challenge. The server runs
//	           CompleteByTap's order (the plaque judged before anything advances, then
//	           sun.Verify's atomic counter, then one statement consuming the invitation) and
//	           creates the device. The app verifies nothing about the chip.
//
// No transactions row is written by any of this (CLAUDE.md §5, "Aktivasyon dokunuşu"): the
// activating tap is not attendance, and the NEXT tap is an ordinary check-in.
class ActivationFlow(
    private val client: AppClient,
    private val keys: DeviceKeys,
    private val state: DeviceState,
) {
    sealed interface LookupResult {
        data class Facts(val facts: LookupReply) : LookupResult
        data class Refused(val problem: Problem) : LookupResult
    }

    sealed interface BindResult {
        data class Bound(val pending: PendingActivation) : BindResult
        /** Back to the code screen with this problem (the invitation is not usable). */
        data class Restart(val problem: Problem) : BindResult
        /** Stay on the consent screen; the person may press again. */
        data class Retry(val problem: Problem) : BindResult
    }

    sealed interface CompleteResult {
        data class Activated(val device: ActiveDevice, val secondDevice: Boolean) : CompleteResult
        /** Stay on the tap step: tap again. [pending] may be a NEW bind (new_key_required). */
        data class TapAgain(val pending: PendingActivation, val problem: Problem) : CompleteResult
        /** The bind no longer holds: back to the consent screen with the same code. */
        data class Rebind(val code: String, val problem: Problem) : CompleteResult
        /** The invitation is not usable: back to the code screen. */
        data class Restart(val problem: Problem) : CompleteResult
    }

    fun lookup(code: String): LookupResult = when (val r = client.lookup(code)) {
        is Outcome.Ok -> LookupResult.Facts(r.value)
        else -> LookupResult.Refused(problemOf(r))
    }

    fun bind(code: String, facts: LookupReply): BindResult {
        val key = keys.generate()
        return when (val r = client.bind(code, key.signer)) {
            is Outcome.Ok -> BindResult.Bound(PendingActivation(code, facts, key, r.value))
            else -> {
                drop(key.alias)
                val problem = problemOf(r)
                if (problem == Problem.BAD_LINK) BindResult.Restart(problem) else BindResult.Retry(problem)
            }
        }
    }

    fun complete(p: PendingActivation, sun: String): CompleteResult {
        when (val r = client.complete(p.code, p.bound, p.key.signer, sun)) {
            is Outcome.Ok -> {
                val previous = state.active()
                val device = ActiveDevice(r.value.deviceId, p.key.alias, p.key.backing, r.value.employeeName)
                state.save(device)
                // The server revoked the person's other credentials in the same transaction
                // (Q40 (a)); this phone's own earlier key, if any, is useless from now on.
                if (previous != null && previous.keyAlias != p.key.alias) drop(previous.keyAlias)
                return CompleteResult.Activated(device, p.facts.secondDevice)
            }
            is Outcome.Refused -> return when (r.code) {
                AppErrorCode.ACTIVATION_TAP_FAILED -> CompleteResult.TapAgain(p, Problem.TAP_FAILED)
                AppErrorCode.ACTIVATION_NEEDS_TOUCH -> CompleteResult.TapAgain(p, Problem.NEEDS_TOUCH)
                AppErrorCode.RATE_LIMITED -> CompleteResult.TapAgain(p, Problem.TOO_MANY)
                AppErrorCode.NEW_KEY_REQUIRED -> rebindWithANewKey(p)
                AppErrorCode.ACTIVATION_NOT_READY -> {
                    drop(p.key.alias)
                    CompleteResult.Rebind(p.code, Problem.NOT_READY)
                }
                AppErrorCode.INVALID_INVITE -> {
                    drop(p.key.alias)
                    CompleteResult.Restart(Problem.BAD_LINK)
                }
                else -> CompleteResult.TapAgain(p, Problem.SERVER)
            }
            // The proof of possession refused: this key cannot complete this bind. Not a clock
            // matter (the text carries no time), so no retry; bind again from the consent screen.
            Outcome.Unauthenticated -> {
                drop(p.key.alias)
                return CompleteResult.Rebind(p.code, Problem.SERVER)
            }
            is Outcome.Failed -> return CompleteResult.TapAgain(p, Problem.NETWORK)
        }
    }

    // new_key_required (M16-07): the public key is already registered -- nothing was consumed,
    // the server rolled the whole transaction back. A fresh pair is bound with the SAME consent
    // the person already gave on this flow, and the person taps once more.
    private fun rebindWithANewKey(p: PendingActivation): CompleteResult {
        drop(p.key.alias)
        return when (val b = bind(p.code, p.facts)) {
            is BindResult.Bound -> CompleteResult.TapAgain(b.pending, Problem.TAP_FAILED)
            is BindResult.Restart -> CompleteResult.Restart(b.problem)
            is BindResult.Retry -> CompleteResult.Rebind(p.code, b.problem)
        }
    }

    /** Drops a bind the person walked away from (Back, a new code): its key is never used. */
    fun abandon(p: PendingActivation) = drop(p.key.alias)

    // Deleting a key is clean-up: an entry the Keystore refuses to delete now is an unused key,
    // never a reason to fail an activation that the server has already completed.
    private fun drop(alias: String) {
        try {
            keys.delete(alias)
        } catch (_: Exception) {
        }
    }

    sealed interface HomeResult {
        data object NotActivated : HomeResult
        data class SetUp(val device: ActiveDevice) : HomeResult
        data object SignedOut : HomeResult
    }

    /**
     * The launch check: is this phone still a device? GET /app/v1/me, signed. Only a 401 that
     * survives AppClient's one clock-corrected retry means signed out (the clock-skew contract);
     * the local device and its key are then dropped and the person activates again. No answer at
     * all keeps the stored device: being offline is not being signed out.
     */
    fun home(): HomeResult {
        val device = state.active() ?: return HomeResult.NotActivated
        val signer = keys.load(device.keyAlias)
        if (signer == null) {
            state.clear()
            return HomeResult.NotActivated
        }
        return when (val r = client.me(device.deviceId, signer)) {
            is Outcome.Ok -> {
                val fresh = device.copy(employeeName = r.value.employeeName)
                if (fresh != device) state.save(fresh)
                HomeResult.SetUp(fresh)
            }
            Outcome.Unauthenticated -> {
                state.clear()
                drop(device.keyAlias)
                HomeResult.SignedOut
            }
            else -> HomeResult.SetUp(device)
        }
    }

    companion object {
        fun problemOf(r: Outcome<*>): Problem = when (r) {
            is Outcome.Ok -> Problem.SERVER
            Outcome.Unauthenticated -> Problem.SERVER
            is Outcome.Failed -> Problem.NETWORK
            is Outcome.Refused -> when (r.code) {
                AppErrorCode.INVALID_INVITE, AppErrorCode.BAD_REQUEST -> Problem.BAD_LINK
                AppErrorCode.RATE_LIMITED -> Problem.TOO_MANY
                else -> Problem.SERVER
            }
        }
    }
}
