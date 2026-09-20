package mt.taptime.relay

import java.io.IOException

/**
 * The relay loop — ADR 0017 §2.1's phone half, and the whole of what this app
 * decides. It is a pipe: begin, then for each C-APDU the server hands out, push it
 * at the chip and post the R-APDU back, until the server says `done`.
 *
 * It computes nothing, keeps nothing and interprets nothing about the bytes. What it
 * DOES decide is how a round ENDS, and every branch of that is a test in
 * RelayLoopTest:
 *
 *   - `done` wins over everything, including a fault in the same body (encode
 *     .Progress.Done: "READ Done BEFORE READING THE ERROR") — reported as
 *     [Outcome.Completed], never re-run, never aborted. ⚠️ Today's server never
 *     PUTS a fault word in a done body (plaqueencode.go's p.Done arm writes the
 *     plain reply and logs the failure); the parser is ready for the shape the
 *     contract describes, and the wire not carrying it is a server-side gap the
 *     orchestrator owns.
 *   - a server fault ends the round with NO abort — and the reason differs by
 *     word; the first version of this comment gave one reason for all seven and
 *     it was wrong for four of them (second-round audit, N2). Store.Abort retires a
 *     live handle and EXPIRES a busy one, so an abort is never a no-op by itself:
 *       refused · unknown-session   the server has already retired the session
 *                                   (finishLocked retires on any step error) or
 *                                   never had one — nothing to abort.
 *       too-many-rounds · server-error · encode-unavailable
 *                                   the refusal came from a gate or a condition
 *                                   AHEAD of the handler (encodeGate's budget, a
 *                                   missing identity, no encoder), and an abort
 *                                   travels the same chain and is refused the same
 *                                   way. The session, if any, lives until the
 *                                   server's 90 s TTL — encodeGate's own decision
 *                                   ("LET IT DIE").
 *       busy                        with a live handle this means ANOTHER step is
 *                                   in flight on it (checkout's ErrBusy); this loop
 *                                   is sequential, so that is somebody else's
 *                                   round, and Store.Abort would expire it. Not
 *                                   ours to kill. (Its other two causes — plaque
 *                                   busy, store full — leave nothing to abort.)
 *       bad-request                 the server could not read what this client
 *                                   sent, i.e. the wire shape here is wrong; a
 *                                   further request on the same shape is a guess.
 *                                   Cost: a live session and its per-plaque lock
 *                                   for up to the 90 s TTL.
 *   - a chip failure (field lost, I/O error) ends the round WITH one abort, so the
 *     plaque's per-UID lock is released now rather than at the 90 s TTL.
 *   - a reply this client cannot read (empty command without `done`, malformed body,
 *     more exchanges than any table this server could have) is a fault, not a
 *     silent stop — and it aborts, because the session on the server is still live.
 *
 * Pure: no android.* import. The chip and the server are interfaces declared beside
 * it, so the tests drive it with a scripted chip and an in-process HTTP server.
 */
class RelayLoop(
    private val server: RelayServer,
    private val chip: Chip,
    private val listener: Listener = Listener.NONE,
) {
    /** Progress for a screen. Carries a count and a step NAME — nothing else. */
    fun interface Listener {
        /**
         * Called before each C-APDU goes to the chip. [exchange] is 1-based;
         * [stepJustCompleted] is the server's `step` word for the previous exchange
         * (`begin` before the first).
         */
        fun onExchange(exchange: Int, stepJustCompleted: String)

        companion object {
            val NONE = Listener { _, _ -> }
        }
    }

    fun run(): Outcome {
        // The bearer handle lives in this local for exactly one round and is
        // unreachable once run() returns; no Outcome carries it.
        var session: String? = null
        var exchanges = 0

        fun abortIfOpen(): Boolean {
            val s = session ?: return false
            session = null
            server.abort(s)
            return true
        }

        var reply = server.begin()
        while (true) {
            when (reply) {
                is Reply.Progress -> {
                    // THE ONE ORDERING THAT MATTERS: done before anything else.
                    if (reply.done) return Outcome.Completed(exchanges, reply.trailingFault)
                    if (session == null) {
                        if (reply.session.isEmpty()) {
                            return Outcome.Protocol(exchanges, "begin answered without a session handle", abortSent = false)
                        }
                        session = reply.session
                    }
                    if (reply.command.isEmpty()) {
                        val sent = abortIfOpen()
                        return Outcome.Protocol(exchanges, "server sent no command and did not say done", sent)
                    }
                    if (exchanges >= MAX_EXCHANGES) {
                        val sent = abortIfOpen()
                        return Outcome.Protocol(exchanges, "more than $MAX_EXCHANGES exchanges without done", sent)
                    }
                    listener.onExchange(exchanges + 1, reply.step)
                    val rapdu = try {
                        chip.transceive(reply.command)
                    } catch (e: ChipLostException) {
                        val sent = abortIfOpen()
                        return Outcome.ChipLost(exchanges, sent)
                    } catch (e: IOException) {
                        val sent = abortIfOpen()
                        return Outcome.ChipError(exchanges, e.message ?: e.javaClass.simpleName, sent)
                    }
                    exchanges++
                    reply = server.step(session!!, rapdu)
                }
                is Reply.Fault -> return Outcome.Faulted(exchanges, reply.status, reply.fault)
                is Reply.Refused -> return Outcome.Refused(exchanges, reply.status)
                is Reply.Malformed -> {
                    val sent = abortIfOpen()
                    return Outcome.Protocol(exchanges, "HTTP ${reply.status}: ${reply.why}", sent)
                }
                is Reply.Unreachable -> {
                    val sent = abortIfOpen()
                    return Outcome.Unreachable(exchanges, reply.why, sent)
                }
            }
        }
    }

    companion object {
        /**
         * A runaway guard, NOT a contract. The server's table is eleven exchanges
         * today (ADR 0017 §5.1 step 8, changekey.appmaster, shipped); the server also
         * bounds every round itself (`too-many-rounds`). This exists so that a server
         * that never says `done` cannot keep a phone transceiving forever.
         */
        const val MAX_EXCHANGES = 16
    }
}

/**
 * How a round ended. Every variant carries the number of exchanges that completed
 * (C-APDU sent AND R-APDU posted back) so a screen can say how far it got, and none
 * carries the session handle.
 */
sealed class Outcome {
    abstract val exchanges: Int

    /**
     * The chip is personalised. [trailingFault] is non-null when the same body that
     * said `done` also carried a fault word: the round succeeded on silicon and
     * something after it (marking the row) did not. Shown separately; never a reason
     * to run again. ⚠️ Today's server never fills it — see the class comment.
     */
    data class Completed(override val exchanges: Int, val trailingFault: String?) : Outcome()

    /** The server refused with one of its fault words. The round is over there; nothing was aborted. */
    data class Faulted(override val exchanges: Int, val status: Int, val fault: String) : Outcome()

    /** The panel chain ahead of the endpoint refused (303 sign-in/origin, 429 budget). Status only. */
    data class Refused(override val exchanges: Int, val status: Int) : Outcome()

    /** The chip left the field. */
    data class ChipLost(override val exchanges: Int, val abortSent: Boolean) : Outcome()

    /** The chip answered with an I/O error other than leaving the field. */
    data class ChipError(override val exchanges: Int, val why: String, val abortSent: Boolean) : Outcome()

    /** The server answered in a shape this client does not understand. */
    data class Protocol(override val exchanges: Int, val why: String, val abortSent: Boolean) : Outcome()

    /** A request never completed. */
    data class Unreachable(override val exchanges: Int, val why: String, val abortSent: Boolean) : Outcome()
}
