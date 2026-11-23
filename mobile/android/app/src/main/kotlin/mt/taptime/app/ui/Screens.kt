package mt.taptime.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import mt.taptime.app.R
import mt.taptime.app.activation.AccentFill
import mt.taptime.app.activation.PairingCode
import mt.taptime.app.activation.Problem
import mt.taptime.app.activation.TapStepParts
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.LookupReply

// THE ACTIVATION SCREENS, in the browser wizard's order (ADR 0026; web/templates/pages/
// activate.templ): [code] -> 1 Welcome (+ terms sheet) -> 2 Get ready -> 3 Tap -> Activation
// complete. The code screen is the app's own (Q40); everything after it is the wizard's twin,
// with the texts of strings.xml. No screen activates anything: "Agree and continue" binds the
// consent to this phone's key, and the first genuine NFC read of an employer's plaque finishes
// (step 3 therefore has no primary button -- the next action is physical) -- or, on a phone that
// cannot tap (M16-23), a venue screen's live QR read by the camera, or the manager's approval
// (the waiting screen).

private const val STEPS = 3

@Composable
private fun header(brand: Brand?) {
    // No logo bytes reach the app yet (M16-07 hands over only the logo's digest and box; the
    // byte route is M16-13's), so the header is the wordmark: ink beside an accent, green otherwise.
    Wordmark(stringResource(R.string.wordmark), stringResource(R.string.tagline), inkWord = AccentFill.of(brand?.accent) != null)
}

@Composable
fun ProblemNotice(p: Problem) {
    val (t, m, h) = when (p) {
        Problem.BAD_LINK -> Triple(R.string.p_bad_link_title, R.string.p_bad_link_message, R.string.p_bad_link_hint)
        Problem.TOO_MANY -> Triple(R.string.p_too_many_title, R.string.p_too_many_message, R.string.p_too_many_hint)
        Problem.SERVER -> Triple(R.string.p_server_title, R.string.p_server_message, R.string.p_server_hint)
        Problem.NETWORK -> Triple(R.string.p_network_title, R.string.p_network_message, R.string.p_network_hint)
        Problem.TAP_FAILED -> Triple(R.string.p_tap_failed_title, R.string.p_tap_failed_message, R.string.p_tap_failed_hint)
        Problem.NEEDS_TOUCH -> Triple(R.string.p_needs_touch_title, R.string.p_needs_touch_message, R.string.p_needs_touch_hint)
        Problem.NOT_READY -> Triple(R.string.p_not_ready_title, R.string.p_not_ready_message, R.string.p_not_ready_hint)
        Problem.SIGNED_OUT -> Triple(R.string.p_signed_out_title, R.string.p_signed_out_message, R.string.p_signed_out_hint)
        Problem.NO_SECURE_KEY -> Triple(R.string.p_no_key_title, R.string.p_no_key_message, R.string.p_no_key_hint)
        Problem.CLOCK -> Triple(R.string.p_clock_title, R.string.p_clock_message, R.string.p_clock_hint)
        Problem.KEY_UNREADABLE -> Triple(R.string.p_key_unreadable_title, R.string.p_key_unreadable_message, R.string.p_key_unreadable_hint)
        Problem.LIVE_QR_FAILED -> Triple(R.string.p_live_failed_title, R.string.p_live_failed_message, R.string.p_live_failed_hint)
        Problem.SCREEN_AVAILABLE -> Triple(R.string.p_screen_available_title, R.string.p_screen_available_message, R.string.p_screen_available_hint)
        Problem.APPROVAL_REJECTED -> Triple(R.string.p_rejected_title, R.string.p_rejected_message, R.string.p_rejected_hint)
        Problem.APPROVAL_EXPIRED -> Triple(R.string.p_expired_title, R.string.p_expired_message, R.string.p_expired_hint)
    }
    Notice(Tone.ALERT, stringResource(t), stringResource(m), stringResource(h))
}

@Composable
fun CodeScreen(problem: Problem?, notALink: Boolean, busy: Boolean, onContinue: (String) -> Unit) {
    var input by remember { mutableStateOf("") }
    Page({ header(null) }) {
        if (problem != null) ProblemNotice(problem)
        Panel {
            DocketLabel(stringResource(R.string.code_label))
            Heading(stringResource(R.string.code_title))
            Body(stringResource(R.string.code_body))
        }
        DocketLabel(stringResource(R.string.code_field))
        BasicTextField(
            value = input,
            onValueChange = { input = it },
            enabled = !busy,
            singleLine = true,
            textStyle = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace, fontSize = 14.sp),
            modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp).background(Palette.Paper)
                .border(1.dp, if (notALink) Palette.Tomato else Palette.Line).padding(horizontal = 12.dp, vertical = 14.dp),
        )
        if (notALink) Notice(Tone.ALERT, null, stringResource(R.string.code_not_a_link))
        PrimaryButton(stringResource(R.string.code_button), accent = null, enabled = !busy && input.isNotBlank()) {
            onContinue(input)
        }
    }
}

@Composable
fun WelcomeScreen(
    facts: LookupReply, problem: Problem?, busy: Boolean,
    onTerms: () -> Unit, onAgree: () -> Unit, onBack: () -> Unit,
) {
    val accent = AccentFill.of(facts.brand.accent)
    Page({ header(facts.brand) }) {
        Progress(stringResource(R.string.step_progress, 1, STEPS, stringResource(R.string.step_welcome)), 1, STEPS, accent != null)
        Panel {
            DocketLabel(facts.businessName, name = true)
            Heading(stringResource(R.string.welcome_hi, facts.employeeName))
            Heading(stringResource(R.string.welcome_sub), muted = true)
        }
        Panel(docket = true) { Body(stringResource(R.string.welcome_panel, facts.businessName)) }
        if (facts.secondDevice) {
            Notice(Tone.WARN, stringResource(R.string.new_phone_title), stringResource(R.string.new_phone_body))
        }
        if (problem != null) ProblemNotice(problem)
        Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
            Body(stringResource(R.string.terms_lead), small = true, center = true)
            BasicText(
                stringResource(R.string.terms_link),
                modifier = Modifier.heightIn(min = 44.dp).clickable(role = Role.Button, onClick = onTerms).padding(vertical = 12.dp),
                style = TextStyle(color = Palette.Ink, fontSize = 14.sp, fontWeight = FontWeight.Bold),
            )
        }
        PrimaryButton(stringResource(R.string.agree), accent, enabled = !busy, onClick = onAgree)
        QuietLink(stringResource(R.string.back), enabled = !busy, onClick = onBack)
    }
}

@Composable
fun TermsSheet(businessName: String, onPrivacy: () -> Unit, onTerms: () -> Unit, onClose: () -> Unit) {
    Page({}) {
        Panel(docket = true) {
            Heading(stringResource(R.string.terms_title))
            Section(stringResource(R.string.terms_who), stringResource(R.string.terms_who_body, businessName))
            Section(stringResource(R.string.terms_what), stringResource(R.string.terms_what_body))
            Section(stringResource(R.string.terms_why), endSentence(stringResource(R.string.terms_why_body, businessName)))
            Body(stringResource(R.string.terms_more))
            QuietLink(stringResource(R.string.terms_privacy), onClick = onPrivacy)
            QuietLink(stringResource(R.string.terms_service), onClick = onTerms)
        }
        QuietLink(stringResource(R.string.terms_close), onClick = onClose)
    }
}

/** The browser's endSentence: a name that already ends in "." ("Ltd.") gets no second one. */
fun endSentence(s: String): String = if (s.endsWith(".")) s else "$s."

@Composable
private fun Section(title: String, body: String) {
    BasicText(title, style = TextStyle(color = Palette.Ink, fontSize = 16.sp, fontWeight = FontWeight.Bold),
        modifier = Modifier.padding(top = 8.dp))
    Body(body)
}

@Composable
fun ReadyScreen(facts: LookupReply, onReady: () -> Unit, onBack: () -> Unit) {
    val accent = AccentFill.of(facts.brand.accent)
    Page({ header(facts.brand) }) {
        Progress(stringResource(R.string.step_progress, 2, STEPS, stringResource(R.string.step_ready)), 2, STEPS, accent != null)
        Panel {
            DocketLabel(stringResource(R.string.ready_label))
            Heading(stringResource(R.string.ready_title))
        }
        Panel(docket = true) {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                BasicText("01", style = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 16.sp, fontWeight = FontWeight.Bold,
                    color = if (accent != null) Palette.Ink else Palette.TappaGreen))
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    BasicText(stringResource(R.string.ready_wifi), style = TextStyle(color = Palette.Ink, fontSize = 16.sp, fontWeight = FontWeight.Bold))
                    val ssid = facts.wifiSsid
                    if (ssid != null) {
                        Body("${stringResource(R.string.ready_wifi_lead)} $ssid ${stringResource(R.string.ready_wifi_tail)}", small = true)
                        Body(stringResource(R.string.ready_wifi_skip), small = true, muted = true)
                    } else {
                        Body(stringResource(R.string.ready_no_wifi), small = true)
                    }
                }
            }
        }
        PrimaryButton(stringResource(R.string.ready_button), accent, enabled = true, onClick = onReady)
        QuietLink(stringResource(R.string.back), onClick = onBack)
    }
}

/** What the phone's NFC can do right now, read by the activity. */
enum class NfcState { READY, OFF, NONE }

@Composable
fun TapScreen(facts: LookupReply, nfc: NfcState, problem: Problem?, busy: Boolean, onCantTap: () -> Unit, onBack: () -> Unit) {
    // M16-23: a phone without a reader is no longer refused; the step says what it can do instead.
    val parts = TapStepParts.of(facts.managerApproval, hasReader = nfc != NfcState.NONE)
    Page({ header(facts.brand) }) {
        Progress(stringResource(R.string.step_progress, 3, STEPS, stringResource(R.string.step_tap)), 3, STEPS,
            AccentFill.of(facts.brand.accent) != null)
        Panel {
            DocketLabel(stringResource(R.string.tap_label))
            Heading(stringResource(R.string.tap_title))
            Body(stringResource(R.string.tap_hold))
            Body(facts.businessName, bold = true)
            Body(stringResource(R.string.tap_not_clock))
        }
        if (nfc == NfcState.OFF) Notice(Tone.WARN, null, stringResource(R.string.nfc_off))
        if (problem != null) ProblemNotice(problem)
        if (parts.noReader) {
            val title = stringResource(R.string.p_no_reader_title)
            val message = stringResource(R.string.p_no_reader_message)
            if (parts.noReaderHint) Notice(Tone.WARN, title, message, stringResource(R.string.p_no_reader_hint))
            else Notice(Tone.WARN, title, message)
        }
        if (parts.plaque) Plaque(stringResource(R.string.wordmark), stringResource(R.string.plaque_hold))
        if (nfc == NfcState.READY) Body(stringResource(R.string.tap_waiting), center = true, muted = busy)
        if (facts.secondDevice) Notice(Tone.WARN, null, stringResource(R.string.tap_signs_out))
        Body(stringResource(R.string.tap_shows), small = true, muted = true, center = true)
        // The secondary way, never the accent's slot (ADR 0023 §2): the step's next action is
        // still physical -- a touch, or the camera on the venue screen.
        if (parts.cantTap) CorrectionLink(stringResource(R.string.cant_tap), enabled = !busy, onClick = onCantTap)
        if (parts.scanScreen) Body(stringResource(R.string.scan_screen_line), small = true, center = true)
        QuietLink(stringResource(R.string.back), enabled = !busy, onClick = onBack)
    }
}

/**
 * M16-23: waiting for the manager's approval -- the code to show them, and the request's end when it
 * came (rejected, expired) with the way to a new request. [notice] is the last poll's passing trouble;
 * the polling goes on under it. The button takes Taptime's green: this is not one of the accent's
 * counted slots (§9).
 */
@Composable
fun WaitingScreen(
    brand: Brand, pairingCode: String?, ended: Problem?, notice: Problem?, busy: Boolean,
    onAskAgain: () -> Unit, onBack: () -> Unit,
) {
    Page({ header(brand) }) {
        Panel {
            DocketLabel(stringResource(R.string.approval_label))
            Heading(stringResource(R.string.approval_title))
            if (ended == null) Body(stringResource(R.string.approval_body))
        }
        if (ended != null) {
            ProblemNotice(ended)
            val again = if (ended == Problem.APPROVAL_EXPIRED) R.string.approval_new_code else R.string.approval_ask_again
            PrimaryButton(stringResource(again), accent = null, enabled = !busy, onClick = onAskAgain)
        } else {
            if (notice != null) ProblemNotice(notice)
            if (pairingCode != null) PairingCodeText(PairingCode.grouped(pairingCode))
            Body(stringResource(R.string.approval_status), center = true, muted = true)
            Body(stringResource(R.string.approval_footnote), small = true, muted = true, center = true)
        }
        QuietLink(stringResource(R.string.back), enabled = !busy, onClick = onBack)
    }
}

@Composable
fun ActivatedScreen(name: String, secondDevice: Boolean, brand: Brand) {
    Page({ header(brand) }) {
        Panel(docket = true) {
            SetupStampRow()
            Heading(stringResource(R.string.activated_title))
            if (name.isNotEmpty()) Body(stringResource(R.string.activated_for, name))
        }
        Notice(Tone.OK, stringResource(R.string.activated_notice_title), stringResource(R.string.activated_notice_body))
        if (secondDevice) Notice(Tone.WARN, null, stringResource(R.string.activated_other_out))
        Body(stringResource(R.string.close_page), muted = true, center = true)
    }
}

@Composable
fun SetUpScreen(name: String, brand: Brand?, hasReader: Boolean) {
    Page({ header(brand) }) {
        Panel(docket = true) {
            SetupStampRow()
            Heading(stringResource(R.string.setup_already_title))
            if (name.isNotEmpty()) Body(stringResource(R.string.setup_signed_in_as, name))
        }
        // M16-23: an activated phone without a reader taps by the venue screen's live QR.
        if (hasReader) Notice(Tone.OK, null, stringResource(R.string.setup_already_notice))
        else Notice(Tone.WARN, stringResource(R.string.p_no_nfc_title), stringResource(R.string.p_no_nfc_message),
            stringResource(R.string.p_no_nfc_hint))
        Body(stringResource(R.string.close_page), muted = true, center = true)
    }
}

@Composable
private fun SetupStampRow() {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.Top) {
        DocketLabel(stringResource(R.string.setup_label))
        Stamp(stringResource(R.string.stamp_activated), Tone.OK)
    }
}

/** A phone that cannot activate at all (no secure key store): the problem, no button. */
@Composable
fun BlockedScreen(problem: Problem) {
    Page({ header(null) }) { ProblemNotice(problem) }
}

/** A problem with "Try again": the key could not be opened (M16-12 hand-over (3)) or the clock is off (M16-16 (4)). */
@Composable
fun RetryScreen(problem: Problem, onRetry: () -> Unit) {
    Page({ header(null) }) {
        ProblemNotice(problem)
        PrimaryButton(stringResource(R.string.try_again), accent = null, enabled = true, onClick = onRetry)
    }
}

@Composable
fun LoadingScreen() {
    Page({ header(null) }) {}
}
