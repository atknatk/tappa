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
        is Outcome.Faulted -> forFault(o.fault)
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
     * Whether the FAULT screen's button may honestly say "Try again". False where the
     * text above says not to: a part-written or row-holding plaque (chip-side and
     * transport interruptions at or past the row), and the three fault words that mean
     * the round is over for THIS plaque (refused, unknown-session, already-encoded).
     * For Unreachable/Protocol the count is
     * conservative — it is incremented before the step is posted, so at exactly
     * ROW the row may or may not exist, and the safe reading is "may".
     */
    fun retryIsSafe(o: Outcome): Boolean = when (o) {
        is Outcome.Completed -> false
        is Outcome.Faulted -> o.fault != "refused" && o.fault != "unknown-session" && o.fault != "already-encoded"
        is Outcome.Refused -> true
        is Outcome.ChipLost -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
        is Outcome.ChipError -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
        is Outcome.Protocol -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
        is Outcome.Unreachable -> o.exchanges < ROW_WRITTEN_AFTER_EXCHANGES
    }

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
