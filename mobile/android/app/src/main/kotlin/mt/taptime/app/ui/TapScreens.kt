package mt.taptime.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import mt.taptime.app.R
import mt.taptime.app.activation.AccentFill
import mt.taptime.app.tap.Buttons
import mt.taptime.app.tap.ResultCopy
import mt.taptime.app.tap.Retry
import mt.taptime.app.tap.TapProblem
import mt.taptime.app.wire.Direction
import mt.taptime.app.wire.TapReply
import mt.taptime.app.wire.TapStartReply

// THE TAP SCREENS (card M16-13), the browser's tap screen and its result, in the browser's order
// and words (web/templates/pages/tap.templ, result.templ; CLAUDE.md §9 -- one screen, one button,
// zero learning):
//
//	tap      "Tapping" / "Hello Maria" / the tapped venue / ONE button -- the predicted direction
//	         ("Clock in", "Clock out") or "Tap" -- and, under a predicted direction only, "Not
//	         right? …" for the other one. The person's name is on screen before anything is recorded:
//	         ADR 0045's accepted shared-phone risk is mitigated by exactly this (user decision
//	         2026-11-18, M16-12 hand-over (5)).
//	result   the venue, the headline and the time, the closing. NO BUTTON (§9): the next action is a
//	         new physical touch of the plaque.
//	problem  the browser's tap problem texts; "Try again" where nothing could have reached the record,
//	         "Open in browser" only where the app cannot go on (not set up, signed out, can't reach).
//
// THE BRAND (ADR 0023 §2): the accent fills the tap button and nothing else -- the header's
// "taptime" turns ink beside it (K-2a); the result screen takes no accent. NO LOGO: no logo bytes
// reach the app (/t/logo/ answers a browser session only, and /app/v1 has no logo route -- a wire
// change shared with iOS, reported on this card), so the header is the wordmark, as on the
// activation screens.

@Composable
private fun tapHeader(accent: AccentFill?) {
    Wordmark(stringResource(R.string.wordmark), stringResource(R.string.tagline), inkWord = accent != null)
}

/** The words of a direction's button: "Clock in", "Clock out", or "Tap" for none (view.go's Words). */
@Composable
private fun words(direction: String): String = when (direction) {
    Direction.IN -> stringResource(R.string.tap_clock_in)
    Direction.OUT -> stringResource(R.string.tap_clock_out)
    else -> stringResource(R.string.tap_tap)
}

/**
 * The tap screen. [onPress] gets the pressed button's direction: the prediction's for the primary
 * button ("" for "Tap", which sends none), the other one for "Not right? …". While [busy] (the
 * reading and the request) both are disabled and the primary one says "Tapping…" (tap.js).
 */
@Composable
fun PlaqueTapScreen(facts: TapStartReply, busy: Boolean, onPress: (String) -> Unit) {
    val accent = AccentFill.of(facts.brand.accent)
    Page({ tapHeader(accent) }) {
        Panel(docket = true) {
            DocketLabel(stringResource(R.string.tap_tapping))
            Heading(stringResource(R.string.tap_hello, facts.employeeName))
            facts.locationName?.let { Body(it, muted = true) }
        }
        Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
            PrimaryButton(if (busy) stringResource(R.string.tap_pending) else words(facts.direction), accent, enabled = !busy) {
                onPress(facts.direction)
            }
            val other = Direction.other(facts.direction)
            if (other.isNotEmpty()) {
                CorrectionLink(stringResource(R.string.tap_not_right, words(other)), enabled = !busy) { onPress(other) }
            }
        }
    }
}

/** The result screen: no accent, no button (§9). */
@Composable
fun ResultScreen(r: TapReply) {
    Page({ tapHeader(null) }) {
        Panel(docket = true) {
            if (r.venue.isNotEmpty()) DocketLabel(r.venue, name = true)
            Heading(line(ResultCopy.headline(r.verdict, r.direction)))
            if (r.at.isNotEmpty()) MonoTime(r.at)
            if (r.practice) Body(line(ResultCopy.Line.PRACTICE), muted = true)
        }
        for (l in ResultCopy.closing(r.verdict, r.direction, r.businessType, r.practice)) {
            if (l.key.startsWith("brand_")) {
                BasicText(line(l), style = TextStyle(color = Palette.Ink, fontFamily = FontFamily.SansSerif, fontSize = 18.sp))
            } else {
                Body(line(l), muted = true)
            }
        }
    }
}

@Composable
private fun line(l: ResultCopy.Line): String = stringResource(
    when (l) {
        ResultCopy.Line.CLOCKED_IN -> R.string.result_clocked_in
        ResultCopy.Line.CLOCKED_OUT -> R.string.result_clocked_out
        ResultCopy.Line.TAPPED -> R.string.result_tapped
        ResultCopy.Line.ALREADY -> R.string.result_already
        ResultCopy.Line.NOT_COUNTED -> R.string.result_not_counted
        ResultCopy.Line.PRACTICE -> R.string.result_practice
        ResultCopy.Line.BRAND_RESTAURANT_IN -> R.string.brand_restaurant_in
        ResultCopy.Line.BRAND_RESTAURANT_OUT -> R.string.brand_restaurant_out
        ResultCopy.Line.BRAND_PRODUCTION_IN -> R.string.brand_production_in
        ResultCopy.Line.BRAND_PRODUCTION_OUT -> R.string.brand_production_out
        ResultCopy.Line.BRAND_OTHER_IN -> R.string.brand_other_in
        ResultCopy.Line.BRAND_OTHER_OUT -> R.string.brand_other_out
        ResultCopy.Line.BRAND_NO_DIRECTION -> R.string.brand_no_direction
        ResultCopy.Line.ALL_DONE -> R.string.result_all_done
        ResultCopy.Line.CLOSE -> R.string.result_close
        ResultCopy.Line.IGNORED -> R.string.result_ignored
        ResultCopy.Line.REJECTED -> R.string.result_rejected
    },
)

/**
 * A tap problem: Taptime's look (the browser's problem pages take no brand), the browser's words, and
 * the iOS twin's buttons ([Buttons]): "Try again" first when there is one, then "Open in browser";
 * without "Try again", "Open in browser" is the primary button and "Start again" / "Set up this
 * phone" the quiet one under it.
 */
@Composable
fun TapProblemScreen(problem: TapProblem, buttons: Buttons, onRetry: () -> Unit, onBrowser: () -> Unit, onActivate: () -> Unit) {
    val (t, m, h) = when (problem) {
        TapProblem.BAD_LINK -> Triple(R.string.tp_bad_link_title, R.string.tp_bad_link_message, R.string.tp_bad_link_hint)
        TapProblem.UNKNOWN_PLAQUE -> Triple(R.string.tp_unknown_title, R.string.tp_unknown_message, R.string.tp_unknown_hint)
        TapProblem.SERVER -> Triple(R.string.tp_server_title, R.string.tp_server_message, R.string.tp_server_hint)
        TapProblem.TOO_MANY -> Triple(R.string.tp_too_many_title, R.string.tp_too_many_message, R.string.tp_too_many_hint)
        TapProblem.STALE -> Triple(R.string.tp_stale_title, R.string.tp_stale_message, R.string.tp_stale_hint)
        TapProblem.FOREIGN -> Triple(R.string.tp_foreign_title, R.string.tp_foreign_message, R.string.tp_foreign_hint)
        TapProblem.UNREACHABLE -> Triple(R.string.tp_unreachable_title, R.string.tp_unreachable_message, R.string.tp_unreachable_hint)
        TapProblem.LOST -> Triple(R.string.tp_lost_title, R.string.tp_lost_message, R.string.tp_lost_hint)
        TapProblem.KEY_UNREADABLE -> Triple(R.string.p_key_unreadable_title, R.string.p_key_unreadable_message, R.string.p_key_unreadable_hint)
        TapProblem.SIGNED_OUT -> Triple(R.string.p_signed_out_title, R.string.p_signed_out_message, R.string.p_signed_out_hint)
        TapProblem.NOT_SET_UP -> Triple(R.string.p_not_set_up_title, R.string.p_not_set_up_message, R.string.p_not_set_up_hint)
        TapProblem.CLOCK -> Triple(R.string.p_clock_title, R.string.p_clock_message, R.string.p_clock_hint)
        // M16-23: a live QR's tap -- the plaque's titles and messages, the screen's hints.
        TapProblem.LIVE_SERVER -> Triple(R.string.tp_server_title, R.string.tp_server_message, R.string.tp_live_server_hint)
        TapProblem.LIVE_TOO_MANY -> Triple(R.string.tp_too_many_title, R.string.tp_too_many_message, R.string.tp_live_too_many_hint)
        TapProblem.LIVE_STALE -> Triple(R.string.tp_stale_title, R.string.tp_stale_message, R.string.tp_live_stale_hint)
        TapProblem.LIVE_FOREIGN -> Triple(R.string.tp_live_foreign_title, R.string.tp_live_foreign_message, R.string.tp_live_foreign_hint)
    }
    Page({ tapHeader(null) }) {
        Notice(Tone.ALERT, stringResource(t), stringResource(m), stringResource(h))
        if (buttons.tryAgain != Retry.NONE) {
            PrimaryButton(stringResource(R.string.try_again), accent = null, enabled = true, onClick = onRetry)
            if (buttons.browser) QuietLink(stringResource(R.string.open_in_browser), onClick = onBrowser)
        } else if (buttons.browser) {
            PrimaryButton(stringResource(R.string.open_in_browser), accent = null, enabled = true, onClick = onBrowser)
        }
        if (buttons.startAgain) QuietLink(stringResource(R.string.start_again), onClick = onActivate)
        if (buttons.setUp) QuietLink(stringResource(R.string.code_title), onClick = onActivate)
    }
}
