package mt.taptime.app.activation

/**
 * THE WAITING SCREEN'S POLL INTERVAL (M16-23 review, ORTA-1). approval/status spends the address's
 * status budget, shared with the browser's polling: ~20 phones waiting behind one venue NAT must fit
 * it, so the base is 5 s (15 minutes = 180 polls a phone). A 429 doubles the interval -- 5, 10, 20,
 * 40, then 60 s at most -- and any answered poll goes back to the base. No answer at all (the phone's
 * network) keeps the base: it spent nothing of the budget. Pure, so the chain is a unit test; the
 * iOS twin is ApprovalPoll in ActivationModel.swift.
 */
object ApprovalPoll {
    const val BASE_MS = 5_000L
    const val MAX_MS = 60_000L

    enum class Outcome {
        /** The server answered (200, or any refusal but 429). */
        ANSWERED,
        /** 429 rate_limited. */
        RATE_LIMITED,
        /** No answer at all. */
        FAILED,
    }

    fun nextDelay(previous: Long, outcome: Outcome): Long = when (outcome) {
        Outcome.RATE_LIMITED -> minOf(maxOf(previous, BASE_MS) * 2, MAX_MS)
        Outcome.ANSWERED, Outcome.FAILED -> BASE_MS
    }

    /** What one poll's result says about the budget (the flow folds 429 and no answer into a notice). */
    fun outcomeOf(r: ActivationFlow.ApprovalResult): Outcome = when {
        r is ActivationFlow.ApprovalResult.Waiting && r.notice == Problem.TOO_MANY -> Outcome.RATE_LIMITED
        r is ActivationFlow.ApprovalResult.Waiting && r.notice == Problem.NETWORK -> Outcome.FAILED
        else -> Outcome.ANSWERED
    }
}
