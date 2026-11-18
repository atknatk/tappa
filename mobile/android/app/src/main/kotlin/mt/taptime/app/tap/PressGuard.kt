package mt.taptime.app.tap

// ONE PRESS PER TAP SCREEN (security review, 2026-11-18). The two buttons of the tap screen -- the
// primary one and "Not right? …" -- are drawn from one composition; two clicks delivered in the same
// frame both see that composition's state, not busy. Judging by the CAPTURED state let both through:
// two POSTs with one signed context, the second recorded as a sun-invalid reject and shown as "Not
// counted". The press therefore judges the LIVE state, on the main thread, and marks it busy before
// anything else runs -- so the second click of the pair finds it busy.
object PressGuard {
    /**
     * Whether a press of the screen drawn for [pressed] may go ahead, given the screen showing NOW:
     * [shown] is the live tap screen's context (null when another screen is up) and [shownBusy] its
     * state. Only the screen the click was drawn for, and only once.
     */
    fun mayPress(shown: TapFlow.Start.Ready?, shownBusy: Boolean, pressed: TapFlow.Start.Ready): Boolean =
        shown != null && !shownBusy && shown === pressed
}
