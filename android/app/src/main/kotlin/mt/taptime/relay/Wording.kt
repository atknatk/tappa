package mt.taptime.relay

/**
 * Human wording for every way a round can end. Kept pure and beside the loop so a
 * test can require that every fault word the server can utter has a sentence here,
 * and that no sentence carries a session handle (there is nowhere to get one from —
 * Outcome does not hold it).
 *
 * The tone is the product's (tappa-brand): short, no blame, says what to do next.
 */
object Wording {
    /** The endpoint's complete fault vocabulary (plaqueencode.go, encodeFaults). */
    val FAULT_WORDS: List<String> = listOf(
        "encode-unavailable",
        "bad-request",
        "unknown-session",
        "busy",
        "refused",
        "already-encoded",
        "too-many-rounds",
        "server-error",
        "suspended",
    )

    fun forFault(fault: String): String = when (fault) {
        "encode-unavailable" ->
            "This server has no plaque encoding switched on. Ask whoever runs it."
        "bad-request" ->
            "The server did not accept what this app sent. Update the app before trying again."
        "unknown-session" ->
            "The server no longer knows this round. If the last step had already gone through, " +
                "the plaque may be encoded — check the plaque list before trying again."
        "busy" ->
            "Another round is already running on this plaque or for this account. Wait a moment, then try again."
        "refused" ->
            // Re-encode now has its own word (already-encoded), so this stays the
            // general "the round failed on its own terms" sentence.
            "The server refused this round. The chip answered in a way the round could not accept. " +
                "Try a fresh plaque; if it keeps happening, tell whoever runs the server."
        "already-encoded" ->
            "This plaque has already been encoded. A plaque can only be encoded once — " +
                "use a fresh one."
        "too-many-rounds" ->
            "The encoding budget for this sign-in is spent. Wait ten minutes, or sign out and in again."
        "server-error" ->
            "Something failed on the server. Try again; if it repeats, ask whoever runs it."
        "suspended" ->
            // ADR 0025 §3, refusal 4: the BUSINESS is suspended, so the panel refuses its
            // writes ahead of the relay. Neither the chip nor this phone did anything
            // wrong, and saying "try a fresh plaque" would waste one. What the plaque
            // holds depends on where the round stopped -- forOutcome adds that.
            "This business's Taptime account is paused, so the server is not taking changes from it right now. " +
                "Nothing is wrong with the plaque or this phone."
        else ->
            "The server answered with a fault this app does not know: $fault."
    }

    fun forStatus(status: Int): String = when (status) {
        303 -> "Not signed in, or the server did not accept this app's origin. Sign in again."
        401, 403 -> "The server refused this sign-in. Sign in again."
        429 -> "The panel's request budget is spent. Wait ten minutes, then try again."
        in 500..599 -> "The server is having trouble (HTTP $status). Try again in a minute."
        else -> "The server answered HTTP $status instead of a round."
    }

    fun forOutcome(o: Outcome): String = when (o) {
        is Outcome.Completed ->
            if (o.trailingFault == null) {
                "Plaque encoded. Hold the next plaque, or sign out."
            } else {
                "Plaque encoded — but the server reported \"${o.trailingFault}\" after the last step. " +
                    "Do NOT encode this plaque again; check the plaque list and tell whoever runs the server."
            }
        is Outcome.Faulted ->
            if (o.fault == "suspended") forFault(o.fault) + " " + afterSuspension(o.exchanges) else forFault(o.fault)
        is Outcome.Refused -> forStatus(o.status)
        is Outcome.ChipLost ->
            "The plaque moved away before the round finished. " + afterInterruption(o.exchanges) + abortNote(o.abortSent)
        is Outcome.ChipError ->
            "The plaque did not answer (${o.why}). " + afterInterruption(o.exchanges) + abortNote(o.abortSent)
        is Outcome.Protocol ->
            "The server answered in a way this app does not understand (${o.why}). Update the app." +
                abortNote(o.abortSent)
        is Outcome.Unreachable ->
            "Could not reach the server (${o.why}). If this happened on the very last step the plaque " +
                "may already be encoded — check the plaque list before trying again." + abortNote(o.abortSent)
    }

    /**
     * What a chip-side interruption leaves behind depends on HOW FAR the round got,
     * and "try again" is the wrong advice for two of the three cases (second-round
     * audit, N4). [exchanges] counts exchanges whose response was posted back and
     * accepted by the server — exact for ChipLost/ChipError, because the previous
     * step call had already answered with the next command before the chip failed.
     *
     *   < ROW      nothing on the server, nothing irreversible on the chip: retry.
     *   ROW..SECRET-1 the inventory row exists (ADR 0017 §5.2, "satır var, çip yok"):
     *              a plain retry dies at the row insert on the same uid and comes back
     *              as `refused`; the chip itself is still recoverable (nothing installed
     *              yet; WriteData may have touched the NDEF file). Needs the server side.
     *   >= SECRET  the plaque's secret is installed, SDM is still OFF (fail-closed
     *              order, ADR 0017 §5.1) and the row is written: the plaque is neither
     *              blank nor finished. ADR 0017 §5.3's recovery path is NOT shipped.
     *              Do not retry; keep the plaque apart.
     */
    fun afterInterruption(exchanges: Int): String = when {
        exchanges < ROW_WRITTEN_AFTER_EXCHANGES ->
            "Nothing was written yet. Hold it flat and still against the phone and try again."
        exchanges < SECRET_INSTALLED_AFTER_EXCHANGES ->
            "The server already holds an inventory row for this plaque, so running it again will be refused. " +
                "Put this plaque aside and tell whoever runs the server; it can still be recovered."
        else ->
            "This plaque is PART-WRITTEN: its secret is installed but it is not finished. " +
                "Do NOT run it again. Keep it apart from blank plaques and tell whoever runs the server."
    }

    /**
     * What a SUSPENSION refusal leaves on the plaque (ADR 0025 §3, refusal 4). The gate
     * refuses the step that carries the chip's latest response, so for
     * `Faulted(exchanges = n)` the CHIP has processed n commands and the SERVER has
     * accepted n − 1 responses (RelayLoop counts an exchange before posting it). The two
     * milestones are effects on different sides, so they are applied to different counts:
     *
     *   n − 1 < ROW        the server never accepted getversion.3's response: no
     *                      inventory row, nothing installed -- the plaque is blank
     *                      and can be encoded once the account is back.
     *   n − 1 ≥ ROW, n < SECRET   the row exists (written when the server accepted the
     *                      4th response) and the secret is not installed: keep the
     *                      plaque apart and tell whoever runs the server.
     *   n ≥ SECRET         the chip processed changekey.sdmfileread: its secret is
     *                      installed and the round is not finished -- PART-WRITTEN.
     *                      ADR 0017 §5.3's recovery path is not shipped (T77), so it
     *                      must be kept apart and never run again.
     *
     * Applying the milestones to n alone would call a blank plaque "apart" at n = 4,
     * where the server had not accepted the row-writing response yet.
     */
    fun afterSuspension(exchanges: Int): String = when {
        exchanges - 1 < ROW_WRITTEN_AFTER_EXCHANGES ->
            "Nothing was written to this plaque; it is still blank. Ask the business owner to contact Taptime. " +
                "Open this app again once the account is switched back on."
        exchanges < SECRET_INSTALLED_AFTER_EXCHANGES ->
            "The server already holds an inventory row for this plaque. Put it aside, keep it apart from blank plaques, " +
                "and tell whoever runs the server."
        else ->
            "This plaque is PART-WRITTEN: its secret is installed but it is not finished. " +
                "Do NOT run it again. Keep it apart from blank plaques and tell whoever runs the server."
    }

    /**
     * Whether the FAULT screen's button may honestly say "Try again". False where the
     * text above says not to: a part-written or row-holding plaque (chip-side and
     * transport interruptions at or past the row), the three fault words that mean
     * the round is over for THIS plaque (refused, unknown-session, already-encoded),
     * and `suspended` -- the business is suspended, so a second round meets the same
     * gate (ADR 0025 §3).
     * For Unreachable/Protocol the count is
     * conservative — it is incremented before the step is posted, so at exactly
     * ROW the row may or may not exist, and the safe reading is "may".
     */
    fun retryIsSafe(o: Outcome): Boolean = when (o) {
        is Outcome.Completed -> false
        is Outcome.Faulted ->
            o.fault != "refused" && o.fault != "unknown-session" && o.fault != "already-encoded" && o.fault != "suspended"
        is Outcome.Refused -> true
        is Outcome.ChipLost -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
        is Outcome.ChipError -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
        is Outcome.Protocol -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
        is Outcome.Unreachable -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
    }

    /** The fault screen's button when the business is suspended (OP-15B, 2nd round, F4). */
    const val CLOSE = "Close"

    /**
     * The FAULT screen's primary button: what pressing it does, in one word, never
     * contradicting the message above it.
     *
     *   suspended          "Close" -- the account is paused, so another round (on this
     *                      plaque or the next) meets the same gate; the button closes the
     *                      app instead of re-arming the reader. "Sign out" stays visible
     *                      beside it (MainActivity shows it on the FAULT screen).
     *   retry is safe      "Try again"
     *   otherwise          "Next plaque" -- the round is over for THIS plaque.
     *
     * Pure, so RelayLoopTest pins it; MainActivity renders it and asks [primaryCloses].
     */
    fun primaryLabel(o: Outcome): String = when {
        o is Outcome.Completed -> "Next plaque"
        o is Outcome.Faulted && o.fault == "suspended" -> CLOSE
        retryIsSafe(o) -> "Try again"
        else -> "Next plaque"
    }

    /** Whether the FAULT screen's button closes the app rather than re-arming the reader. */
    fun primaryCloses(o: Outcome): Boolean = primaryLabel(o) == CLOSE

    /**
     * After this many completed exchanges the tags row exists: the accept of the
     * third GetVersion frame writes it (internal/encode/driver.go,
     * acceptVersionFrame3AndWriteRow). Pinned to the server's table by
     * ContractPinTest.theTwoMilestones_matchDriverGo.
     */
    const val ROW_WRITTEN_AFTER_EXCHANGES = 4

    /** After this many completed exchanges the plaque's secret is installed (the server's changekey.sdmfileread step accepted). Same pin. */
    const val SECRET_INSTALLED_AFTER_EXCHANGES = 9

    private fun abortNote(sent: Boolean): String =
        if (sent) " The round was cancelled on the server." else ""
}
