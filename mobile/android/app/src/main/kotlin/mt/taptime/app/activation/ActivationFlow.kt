package mt.taptime.app.activation

import mt.taptime.app.wire.AppClient
import mt.taptime.app.wire.AppErrorCode
import mt.taptime.app.wire.ApprovalReply
import mt.taptime.app.wire.BindReply
import mt.taptime.app.wire.CompleteReply
import mt.taptime.app.wire.LookupReply
import mt.taptime.app.wire.Outcome

/** The screens' closed set of problems; each one is a fixed text (strings.xml), never a server value. */
enum class Problem {
    BAD_LINK, TOO_MANY, SERVER, NETWORK, TAP_FAILED, NEEDS_TOUCH, NOT_READY, SIGNED_OUT, NO_SECURE_KEY,
    /** M16-16 hand-over (4), the iOS twin's clock: a 401 while the phone's clock is a day or more off. */
    CLOCK,
    /** M16-12 hand-over (3): the Keystore did not hand out the device key (iOS's keyUnreadable). */
    KEY_UNREADABLE,
    /** M16-23: a venue screen's live QR did not finish setup (every refusal of it is one code, §4.7). */
    LIVE_QR_FAILED,
    /** M16-23: screen_available -- a live QR screen runs at the workplace, so no manager approval. */
    SCREEN_AVAILABLE,
    /** M16-23: the manager rejected this phone. */
    APPROVAL_REJECTED,
    /** M16-23: no approval in time (15 minutes), or the request is gone. */
    APPROVAL_EXPIRED,
}

/** The bind the tap step holds: the code, the fresh key and the server's challenge. Memory only. */
class PendingActivation(val code: String, val facts: LookupReply, val key: DeviceKey, val bound: BindReply) {
    /** The same bind once the server said a screen runs there (screen_available): no approval path. */
    fun withoutApproval() = PendingActivation(code, facts.copy(managerApproval = false), key, bound)
}

/**
 * What step 3 draws around its fixed words (M16-23), from the lookup and the phone. [plaque]: the
 * plaque drawing and its reader (a phone with an NFC reader); [noReader]: the "can't read the
 * plaque" notice in its place, with [noReaderHint] -- "Or ask your manager…" -- only when the
 * manager can; [cantTap]: the secondary "My phone can't tap" (manager approval offered);
 * [scanScreen]: otherwise the line that sends the person to the venue screen's code.
 */
data class TapStepParts(
    val plaque: Boolean,
    val noReader: Boolean,
    val noReaderHint: Boolean,
    val cantTap: Boolean,
    val scanScreen: Boolean,
) {
    companion object {
        fun of(managerApproval: Boolean, hasReader: Boolean) = TapStepParts(
            plaque = hasReader,
            noReader = !hasReader,
            noReaderHint = !hasReader && managerApproval,
            cantTap = managerApproval,
            scanScreen = !managerApproval,
        )
    }
}

/** The pairing code as the screens show it: "482913" -> "482 913". */
object PairingCode {
    fun grouped(code: String): String = if (code.length == 6) code.substring(0, 3) + " " + code.substring(3) else code
}

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
//	           OR (M16-23, a phone that cannot tap) a venue screen's LIVE QR read by the camera:
//	           its `/q` query goes to complete as `liveQR` instead of `sun`, the server opens the
//	           token in place of sun.Verify. OR the manager's approval: "My phone can't tap" opens
//	           a request (activate/approval) whose pairing code the manager matches in the panel;
//	           the waiting screen polls approval/status, and an approved answer IS the
//	           activation -- the server created the device in that call.
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
        /** This phone's Keystore is software only (KeyGate): it cannot be set up. Nothing was sent. */
        data object Unsupported : BindResult
    }

    sealed interface CompleteResult {
        data class Activated(val device: ActiveDevice, val secondDevice: Boolean) : CompleteResult
        /** Stay on the tap step: tap again. [pending] may be a NEW bind (new_key_required). */
        data class TapAgain(val pending: PendingActivation, val problem: Problem) : CompleteResult
        /** The bind no longer holds: back to the consent screen with the same code. */
        data class Rebind(val code: String, val problem: Problem) : CompleteResult {
            // The invite code is a credential (§7): never in a printed value.
            override fun toString() = "Rebind(code=<redacted>, problem=$problem)"
        }
        /** The invitation is not usable: back to the code screen. */
        data class Restart(val problem: Problem) : CompleteResult
    }

    fun lookup(code: String): LookupResult = when (val r = client.lookup(code)) {
        is Outcome.Ok -> LookupResult.Facts(r.value)
        else -> LookupResult.Refused(problemOf(r))
    }

    fun bind(code: String, facts: LookupReply): BindResult {
        val key = keys.generate()
        if (!KeyGate.accepts(key.backing)) {
            drop(key.alias)
            return BindResult.Unsupported
        }
        return when (val r = client.bind(code, key.signer)) {
            is Outcome.Ok -> BindResult.Bound(PendingActivation(code, facts, key, r.value))
            else -> {
                drop(key.alias)
                val problem = problemOf(r)
                if (problem == Problem.BAD_LINK) BindResult.Restart(problem) else BindResult.Retry(problem)
            }
        }
    }

    fun complete(p: PendingActivation, sun: String): CompleteResult =
        finish(p, client.complete(p.code, p.bound, p.key.signer, sun), live = false)

    /** M16-23: a venue screen's live QR in place of the plaque: its `/q` query as `liveQR`. */
    fun completeLive(p: PendingActivation, liveQuery: String): CompleteResult =
        finish(p, client.completeLive(p.code, p.bound, p.key.signer, liveQuery), live = true)

    /** The activating read, whichever opened it: a plaque's SUN query or a venue screen's live QR. */
    fun complete(p: PendingActivation, link: TapLink): CompleteResult = when (link) {
        is TapLink.Plaque -> complete(p, link.query)
        is TapLink.LiveQr -> completeLive(p, link.query)
    }

    private fun finish(p: PendingActivation, r: Outcome<CompleteReply>, live: Boolean): CompleteResult {
        // Every refusal of a live QR read is activation_tap_failed (one code, §4.7); the app knows it
        // sent a screen's code, so the text says to scan the screen again, not to tap the plaque.
        val readFailed = if (live) Problem.LIVE_QR_FAILED else Problem.TAP_FAILED
        when (r) {
            is Outcome.Ok -> {
                val (device, second) = activated(p, r.value.deviceId, r.value.employeeName)
                return CompleteResult.Activated(device, second)
            }
            // Where each answer leads is the iOS twin's (ActivationModel.completeRefused): the tap
            // step for a read the server judged, the consent step for anything else it refused.
            is Outcome.Refused -> return when (r.code) {
                AppErrorCode.ACTIVATION_TAP_FAILED, AppErrorCode.UNKNOWN_PLAQUE, AppErrorCode.FOREIGN_TENANT,
                AppErrorCode.STALE_CONTEXT -> CompleteResult.TapAgain(p, readFailed)
                AppErrorCode.ACTIVATION_NEEDS_TOUCH -> CompleteResult.TapAgain(p, if (live) readFailed else Problem.NEEDS_TOUCH)
                AppErrorCode.RATE_LIMITED -> CompleteResult.TapAgain(p, Problem.TOO_MANY)
                AppErrorCode.NEW_KEY_REQUIRED -> rebindWithANewKey(p, readFailed)
                AppErrorCode.ACTIVATION_NOT_READY -> {
                    drop(p.key.alias)
                    CompleteResult.Rebind(p.code, Problem.NOT_READY)
                }
                AppErrorCode.INVALID_INVITE -> {
                    drop(p.key.alias)
                    CompleteResult.Restart(Problem.BAD_LINK)
                }
                else -> {
                    drop(p.key.alias)
                    CompleteResult.Rebind(p.code, Problem.SERVER)
                }
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

    // A finished activation -- complete's answer, or an approved request's (M16-23): the device is
    // saved, and this phone's own earlier key, if any, is dropped. The server revoked the person's
    // other credentials in the same transaction (Q40 (a)).
    private fun activated(p: PendingActivation, deviceId: String, employeeName: String): Pair<ActiveDevice, Boolean> {
        val previous = state.active()
        val device = ActiveDevice(deviceId, p.key.alias, p.key.backing, employeeName)
        state.save(device)
        if (previous != null && previous.keyAlias != p.key.alias) drop(previous.keyAlias)
        return device to p.facts.secondDevice
    }

    // new_key_required (M16-07): the public key is already registered -- nothing was consumed,
    // the server rolled the whole transaction back. A fresh pair is bound with the SAME consent
    // the person already gave on this flow, and the person taps once more.
    private fun rebindWithANewKey(p: PendingActivation, problem: Problem): CompleteResult {
        drop(p.key.alias)
        return when (val b = bind(p.code, p.facts)) {
            is BindResult.Bound -> CompleteResult.TapAgain(b.pending, problem)
            is BindResult.Restart -> CompleteResult.Restart(b.problem)
            is BindResult.Retry -> CompleteResult.Rebind(p.code, b.problem)
            BindResult.Unsupported -> CompleteResult.Restart(Problem.NO_SECURE_KEY)
        }
    }

    // --- the manager's approval (M16-23) ------------------------------------------------------

    sealed interface ApprovalResult {
        /**
         * The waiting screen: [approval] carries the pairing code. [notice] is a passing trouble of
         * the last poll (no answer, too many, the server's) -- the screen keeps waiting and polling.
         */
        data class Waiting(val pending: PendingActivation, val approval: ApprovalReply.Waiting, val notice: Problem? = null) : ApprovalResult
        /** Approved: the server created the device in that call, exactly as a successful complete. */
        data class Activated(val device: ActiveDevice, val secondDevice: Boolean) : ApprovalResult
        /** Rejected or expired: nothing was set up; the screen offers a new request. */
        data class Ended(val pending: PendingActivation, val problem: Problem) : ApprovalResult
        /**
         * Back to the tap step with [problem]. [pending] may be the same bind without the approval
         * path (screen_available) or a NEW bind (new_key_required).
         */
        data class TapStep(val pending: PendingActivation, val problem: Problem) : ApprovalResult
        /** The bind no longer holds: back to the consent screen with the same code. */
        data class Rebind(val code: String, val problem: Problem) : ApprovalResult {
            override fun toString() = "Rebind(code=<redacted>, problem=$problem)"
        }
        /** The invitation is not usable: back to the code screen. */
        data class Restart(val problem: Problem) : ApprovalResult
    }

    /** "My phone can't tap": opens a request; an earlier open one of this invitation is withdrawn. */
    fun requestApproval(p: PendingActivation): ApprovalResult =
        when (val r = client.approval(p.code, p.bound, p.key.signer)) {
            is Outcome.Ok -> approvalAnswer(p, r.value)
            is Outcome.Refused -> when (r.code) {
                AppErrorCode.SCREEN_AVAILABLE -> ApprovalResult.TapStep(p.withoutApproval(), Problem.SCREEN_AVAILABLE)
                AppErrorCode.RATE_LIMITED -> ApprovalResult.TapStep(p, Problem.TOO_MANY)
                else -> approvalRefused(p, r.code)
            }
            Outcome.Unauthenticated -> {
                drop(p.key.alias)
                ApprovalResult.Rebind(p.code, Problem.SERVER)
            }
            is Outcome.Failed -> ApprovalResult.TapStep(p, Problem.NETWORK)
        }

    /**
     * One poll of the waiting screen. A passing trouble -- no answer, rate_limited, server_error --
     * keeps [last] on screen and the polling going; the request's own end (approved, rejected,
     * expired) or a bind that no longer holds leaves the screen.
     */
    fun pollApproval(p: PendingActivation, last: ApprovalReply.Waiting): ApprovalResult =
        when (val r = client.approvalStatus(p.code, p.bound, p.key.signer)) {
            is Outcome.Ok -> approvalAnswer(p, r.value)
            is Outcome.Refused -> when (r.code) {
                AppErrorCode.RATE_LIMITED -> ApprovalResult.Waiting(p, last, Problem.TOO_MANY)
                AppErrorCode.SERVER_ERROR -> ApprovalResult.Waiting(p, last, Problem.SERVER)
                AppErrorCode.SCREEN_AVAILABLE -> ApprovalResult.TapStep(p.withoutApproval(), Problem.SCREEN_AVAILABLE)
                else -> approvalRefused(p, r.code)
            }
            Outcome.Unauthenticated -> {
                drop(p.key.alias)
                ApprovalResult.Rebind(p.code, Problem.SERVER)
            }
            is Outcome.Failed -> ApprovalResult.Waiting(p, last, Problem.NETWORK)
        }

    private fun approvalAnswer(p: PendingActivation, reply: ApprovalReply): ApprovalResult = when (reply) {
        is ApprovalReply.Waiting -> ApprovalResult.Waiting(p, reply)
        is ApprovalReply.Approved -> activated(p, reply.deviceId, reply.employeeName).let { (d, second) ->
            ApprovalResult.Activated(d, second)
        }
        ApprovalReply.Rejected -> ApprovalResult.Ended(p, Problem.APPROVAL_REJECTED)
        ApprovalReply.Expired -> ApprovalResult.Ended(p, Problem.APPROVAL_EXPIRED)
    }

    // The answers the approval routes share with complete's: a moved binding goes back to consent,
    // a dead invitation to the code screen, a taken key binds a fresh one (the person then asks
    // again from the tap step -- the request belonged to the old key).
    private fun approvalRefused(p: PendingActivation, code: AppErrorCode?): ApprovalResult = when (code) {
        AppErrorCode.NEW_KEY_REQUIRED -> when (val c = rebindWithANewKey(p, Problem.SERVER)) {
            is CompleteResult.TapAgain -> ApprovalResult.TapStep(c.pending, c.problem)
            is CompleteResult.Rebind -> ApprovalResult.Rebind(c.code, c.problem)
            is CompleteResult.Restart -> ApprovalResult.Restart(c.problem)
            is CompleteResult.Activated -> ApprovalResult.Activated(c.device, c.secondDevice)
        }
        AppErrorCode.ACTIVATION_NOT_READY -> {
            drop(p.key.alias)
            ApprovalResult.Rebind(p.code, Problem.NOT_READY)
        }
        AppErrorCode.INVALID_INVITE -> {
            drop(p.key.alias)
            ApprovalResult.Restart(Problem.BAD_LINK)
        }
        else -> {
            drop(p.key.alias)
            ApprovalResult.Rebind(p.code, Problem.SERVER)
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
        /**
         * The Keystore refused to load or sign with the key (M16-12 hand-over (3), iOS's
         * keyUnreadable): "Try again" runs home again. The record and the key are KEPT.
         */
        data object KeyUnreadable : HomeResult
        /**
         * Two 401s while the last answer's Date was more than a day off this phone's clock
         * (ServerClock.lastAnswerWasFarOff): the phone's clock, not a signed-out device -- iOS's
         * "Check your phone's clock" (M16-16 hand-over (4)). "Try again" runs home again.
         */
        data object Clock : HomeResult
    }

    /**
     * The launch check: is this phone still a device? GET /app/v1/me, signed. Only a 401 that
     * survives AppClient's one clock-corrected retry means signed out (the clock-skew contract):
     * the phone shows "This phone was signed out" and the way to activate again. No answer at all
     * keeps the stored device: being offline is not being signed out.
     *
     * A 401 NEVER DELETES THE RECORD OR THE KEY (security review, 2026-11-18; the iOS twin keeps
     * the same rule). A 401 is the answer to a revoked device, but also to a server-side fault --
     * an RLS or GRANT regression that makes the resolver miss every device answers 401 to every
     * phone at once -- and a deleted Keystore key cannot come back. Keeping it costs nothing: the
     * server refuses a revoked key whatever the phone keeps. Only a NEW successful activation
     * replaces the record (complete). The one case that is dropped is a record whose Keystore
     * entry is already gone: that state is permanent, nothing can ever sign for it.
     */
    fun home(): HomeResult {
        val device = state.active() ?: return HomeResult.NotActivated
        // ONE retry, then the "Try again" screen -- never a deletion (M16-12 hand-over (3); the iOS
        // twin's start()): a Keystore that fails once usually answers the next time, and a deleted
        // key never would. Until this card a throwing Keystore ended at the code screen with the
        // server's text, which was both wrong and a dead end.
        val signer = try {
            keys.load(device.keyAlias)
        } catch (_: Exception) {
            try {
                keys.load(device.keyAlias)
            } catch (_: Exception) {
                return HomeResult.KeyUnreadable
            }
        }
        if (signer == null) {
            state.clear()
            return HomeResult.NotActivated
        }
        val r = try {
            client.me(device.deviceId, signer)
        } catch (_: Exception) {
            // Only the signer throws on this path (AppClient turns transport failures into
            // Outcome.Failed and replies it cannot read into Outcome.Failed too).
            return HomeResult.KeyUnreadable
        }
        return when (r) {
            is Outcome.Ok -> {
                val fresh = device.copy(employeeName = r.value.employeeName)
                if (fresh != device) state.save(fresh)
                HomeResult.SetUp(fresh)
            }
            Outcome.Unauthenticated -> if (client.clock.lastAnswerWasFarOff) HomeResult.Clock else HomeResult.SignedOut
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
