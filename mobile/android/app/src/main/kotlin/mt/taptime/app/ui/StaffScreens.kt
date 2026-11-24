package mt.taptime.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import mt.taptime.app.R
import mt.taptime.app.activation.AccentFill
import mt.taptime.app.staff.ClaimWords
import mt.taptime.app.wire.Brand
import mt.taptime.app.wire.HoursRecord
import mt.taptime.app.wire.HoursReply
import mt.taptime.app.wire.MeReply
import mt.taptime.app.wire.OpenClaim
import mt.taptime.app.wire.SentClaim
import mt.taptime.app.wire.UsualShift

// The staff app's own screens (M16-44/46/47/48; the user's §9 answers of 2026-11-23): the home
// screen that replaces "This phone is already set up", Settings, and "Missing times". The tap and
// result screens are untouched: clocking in and out is still the plaque's.

@Composable
private fun staffHeader(brand: Brand?) {
    Wordmark(stringResource(R.string.wordmark), stringResource(R.string.tagline), inkWord = AccentFill.of(brand?.accent) != null)
}

@Composable
private fun shiftLine(s: UsualShift): String =
    if (s.overnight) stringResource(R.string.shift_usual_overnight, s.start, s.end, s.venue)
    else stringResource(R.string.shift_usual, s.start, s.end, s.venue)

/** The home screen: who, at work or not, the usual shift, and two quiet links. No button. */
@Composable
fun HomeScreen(me: MeReply, hasReader: Boolean, onMissing: () -> Unit, onSettings: () -> Unit, onHours: () -> Unit = {}) {
    val p = me.presence
    Page({ staffHeader(me.brand) }) {
        Panel(docket = true) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.Top) {
                DocketLabel(stringResource(if (p != null) R.string.home_at_work else R.string.home_not_in))
                if (p != null) Stamp(stringResource(R.string.home_stamp_in), Tone.OK)
            }
            Heading(stringResource(R.string.home_hello, me.employeeName))
            if (p != null) Body(stringResource(R.string.home_in_since, p.inSinceLocal))
            me.shift?.let { Body(shiftLine(it), small = true, muted = true) }
        }
        if (hasReader) Body(stringResource(if (p != null) R.string.home_tap_out else R.string.home_tap_in), center = true)
        else Notice(Tone.WARN, stringResource(R.string.p_no_nfc_title), stringResource(R.string.p_no_nfc_message),
            stringResource(R.string.p_no_nfc_hint))
        if (me.hoursWeeks > 0) QuietLink(stringResource(R.string.home_hours), onClick = onHours)
        if (me.missingTimes > 0) QuietLink(stringResource(R.string.home_missing, me.missingTimes), onClick = onMissing)
        QuietLink(stringResource(R.string.home_settings), onClick = onSettings)
    }
}

/** One switch row: the words, and a box that says on or off. */
@Composable
private fun SwitchRow(text: String, hint: String?, on: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    val state = if (on) "on" else "off"
    Row(
        Modifier.fillMaxWidth().heightIn(min = 48.dp).background(Palette.Paper).border(1.dp, Palette.Line)
            .clickable(enabled = enabled, role = Role.Switch) { onChange(!on) }
            .semantics { stateDescription = state }.padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Body(text, bold = true)
            if (hint != null) Body(hint, small = true, muted = true)
        }
        Box(
            Modifier.size(22.dp).border(2.dp, if (on) Palette.TappaGreen else Palette.Line)
                .background(if (on) Palette.TappaGreen else Palette.Paper),
        )
    }
}

/** Settings: the person, the usual shift, and the two switches (both off by default). */
@Composable
fun SettingsScreen(
    me: MeReply, remind: Boolean, timer: Boolean, notificationsOff: Boolean,
    onRemind: (Boolean) -> Unit, onTimer: (Boolean) -> Unit, onBack: () -> Unit,
) {
    Page({ staffHeader(me.brand) }) {
        Panel {
            DocketLabel(stringResource(R.string.settings_label))
            Heading(me.employeeName)
            me.shift?.let { Body(shiftLine(it), small = true, muted = true) }
            me.presence?.shiftEndsAtLocal?.let { Body(stringResource(R.string.shift_today_ends, it), small = true, muted = true) }
        }
        SwitchRow(stringResource(R.string.settings_remind), stringResource(R.string.settings_remind_hint), remind, true, onRemind)
        SwitchRow(stringResource(R.string.settings_timer), null, timer, true, onTimer)
        if (notificationsOff) Notice(Tone.WARN, null, stringResource(R.string.settings_notifications_off))
        QuietLink(stringResource(R.string.back), onClick = onBack)
    }
}

/** What the last send answered, for the screen's notice. */
enum class ClaimNotice { SENT, EXPIRED, RESOLVED, NOT_ACTIVE, STILL_OPEN, NETWORK }

@Composable
private fun claimLine(l: ClaimWords.Line): String = stringResource(l.id, *l.args.toTypedArray())

/** "Missing times": the browser card's words per record, and the person's own claims. */
@Composable
fun MissingTimesScreen(
    brand: Brand?, open: List<OpenClaim>, sent: List<SentClaim>, notice: ClaimNotice?,
    problems: Map<String, String>, busy: Boolean,
    onSend: (OpenClaim, String) -> Unit, onBack: () -> Unit,
) {
    val locale = LocalConfiguration.current.locales[0]
    Page({ staffHeader(brand) }) {
        DocketLabel(stringResource(R.string.missing_label))
        when (notice) {
            ClaimNotice.SENT -> Notice(Tone.OK, null, stringResource(R.string.claim_sent))
            ClaimNotice.EXPIRED -> Notice(Tone.ALERT, stringResource(R.string.claim_expired_title),
                stringResource(R.string.claim_expired_message), stringResource(R.string.claim_tell_manager))
            ClaimNotice.RESOLVED -> Notice(Tone.ALERT, stringResource(R.string.claim_resolved_title),
                stringResource(R.string.claim_resolved_message), stringResource(R.string.claim_resolved_hint))
            ClaimNotice.NOT_ACTIVE -> Notice(Tone.ALERT, stringResource(R.string.claim_not_active_title),
                stringResource(R.string.claim_not_active_message), stringResource(R.string.claim_tell_manager))
            ClaimNotice.STILL_OPEN -> Notice(Tone.WARN, stringResource(R.string.claim_still_open_title),
                stringResource(R.string.claim_still_open_message))
            ClaimNotice.NETWORK -> ProblemNotice(mt.taptime.app.activation.Problem.NETWORK)
            null -> Unit
        }
        if (open.isEmpty()) Body(stringResource(R.string.missing_none), muted = true)
        for (c in open) ClaimCard(c, locale, problems[c.context], busy, onSend)
        if (sent.isNotEmpty()) {
            DocketLabel(stringResource(R.string.missing_sent_label))
            for (s in sent) {
                Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    val kind = stringResource(if (s.kind == "in") R.string.missing_kind_in else R.string.missing_kind_out)
                    BasicText(stringResource(R.string.missing_sent_row, ClaimWords.date(s.anchorDate, locale), kind, s.claimedAt),
                        style = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace, fontSize = 14.sp))
                    Body(claimLine(ClaimWords.state(s.state, s.approvedAt)), small = true, muted = true)
                }
            }
        }
        QuietLink(stringResource(R.string.back), enabled = !busy, onClick = onBack)
    }
}

@Composable
private fun ClaimCard(c: OpenClaim, locale: java.util.Locale, problem: String?, busy: Boolean, onSend: (OpenClaim, String) -> Unit) {
    var at by remember(c.context) { mutableStateOf(c.prefill ?: "") }
    Panel(docket = true) {
        Body(claimLine(ClaimWords.lead(c.kind, c.day, c.anchorDate, c.anchorAt, locale)))
        Body(stringResource(ClaimWords.question(c.kind)), bold = true)
        BasicTextField(
            value = at,
            onValueChange = { at = it.take(5) },
            enabled = !busy,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            textStyle = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace, fontSize = 20.sp),
            modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp).background(Palette.Paper)
                .border(1.dp, if (problem != null) Palette.Tomato else Palette.Line).padding(horizontal = 12.dp, vertical = 12.dp),
        )
        if (problem != null) Body(problem, small = true)
        PrimaryButton(stringResource(R.string.claim_send), accent = null, enabled = !busy) { onSend(c, at.trim()) }
        Body(stringResource(R.string.claim_skip), small = true, muted = true, center = true)
    }
}

/** The outside-window sentence for one record, with the server's two clocks. */
@Composable
fun outsideWindowText(c: OpenClaim): String =
    stringResource(R.string.claim_outside, stringResource(ClaimWords.window(c.fromInclusive, c.toInclusive), c.windowFrom, c.windowTo))

// --- My hours (M16-33): the person's own week, in plain words (the card's recommendation (b)) ---

@Composable
private fun duration(minutes: Int): String = stringResource(R.string.hours_duration, minutes / 60, minutes % 60)

private fun dayOf(date: String, locale: java.util.Locale): String = ClaimWords.date(date, locale)

@Composable
private fun stateWord(state: String): String = stringResource(
    when (state) {
        "approved" -> R.string.hours_state_approved
        "waiting" -> R.string.hours_state_waiting
        "repeat" -> R.string.hours_state_repeat
        else -> R.string.hours_state_rejected
    },
)

@Composable
private fun recordLine(r: HoursRecord, locale: java.util.Locale): String {
    val parts = mutableListOf(dayOf(r.date, locale), r.at)
    when (r.direction) {
        "in" -> parts += stringResource(R.string.hours_dir_in)
        "out" -> parts += stringResource(R.string.hours_dir_out)
    }
    if (r.venue.isNotEmpty()) parts += r.venue
    return parts.joinToString(" · ")
}

@Composable
private fun recordNote(r: HoursRecord): String {
    val parts = mutableListOf(stateWord(r.state))
    when (r.channel) {
        "manual" -> parts += stringResource(R.string.hours_channel_manual)
        "qr" -> parts += stringResource(R.string.hours_channel_qr)
        "qr_live" -> parts += stringResource(R.string.hours_channel_qr_live)
        "other" -> parts += stringResource(R.string.hours_channel_other)
    }
    when (r.claim) {
        "pending" -> parts += stringResource(R.string.hours_claim_pending)
        "approved" -> parts += stringResource(R.string.hours_claim_approved)
        "rejected" -> parts += stringResource(R.string.hours_claim_rejected)
    }
    return parts.joinToString(" · ")
}

/**
 * "My hours": the week's counted total, what keeps it from being complete, the days, and every
 * record with its state in words. Nothing is kept on the phone (the card's rule): the screen shows
 * the answer it was given and reads again when it opens.
 */
@Composable
fun HoursScreen(
    brand: Brand?, reply: HoursReply?, records: List<HoursRecord>, busy: Boolean, network: Boolean,
    onPrev: () -> Unit, onNext: () -> Unit, onMore: () -> Unit, onBack: () -> Unit,
) {
    val locale = LocalConfiguration.current.locales[0]
    val res = androidx.compose.ui.platform.LocalContext.current.resources
    Page({ staffHeader(brand) }) {
        DocketLabel(stringResource(R.string.hours_label))
        if (network) ProblemNotice(mt.taptime.app.activation.Problem.NETWORK)
        if (reply != null) {
            val t = reply.totals
            Panel(docket = true) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    DocketLabel(stringResource(R.string.hours_week_of, dayOf(reply.week, locale)))
                    DocketLabel(
                        if (reply.weeksVisible <= 1) stringResource(R.string.hours_window_1)
                        else stringResource(R.string.hours_window_n, reply.weeksVisible),
                    )
                }
                Body(stringResource(R.string.hours_counted_label), small = true, muted = true)
                Heading(duration(t.workedMinutes))
                if (t.awaitingMinutes > 0) Body(stringResource(R.string.hours_awaiting, duration(t.awaitingMinutes)), small = true)
                if (t.openEntries > 0) Body(res.getQuantityString(R.plurals.hours_open, t.openEntries, t.openEntries), small = true)
                if (t.checkInMissing > 0) Body(res.getQuantityString(R.plurals.hours_in_missing, t.checkInMissing, t.checkInMissing), small = true)
                if (t.truncated) Body(stringResource(R.string.hours_truncated), small = true)
            }
            val waiting = buildList {
                if (reply.waitingRecords > 0) add(res.getQuantityString(R.plurals.hours_waiting_records, reply.waitingRecords, reply.waitingRecords))
                if (reply.waitingClaims > 0) add(res.getQuantityString(R.plurals.hours_waiting_claims, reply.waitingClaims, reply.waitingClaims))
            }
            if (waiting.isNotEmpty()) Notice(Tone.WARN, null, stringResource(R.string.hours_waiting_line, waiting.joinToString(" · ")))
            DocketLabel(stringResource(R.string.hours_days_label))
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                for (d in reply.days) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        BasicText(dayOf(d.date, locale), style = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace, fontSize = 14.sp))
                        BasicText(if (d.workedMinutes > 0) duration(d.workedMinutes) else "—",
                            style = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace, fontSize = 14.sp))
                    }
                }
            }
            DocketLabel(stringResource(R.string.hours_records_label))
            if (records.isEmpty()) Body(stringResource(R.string.hours_none), muted = true)
            for (r in records) {
                Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    BasicText(recordLine(r, locale), style = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace, fontSize = 14.sp))
                    Body(recordNote(r), small = true, muted = r.state == "approved")
                }
            }
            if (reply.nextCursor != null) QuietLink(stringResource(R.string.hours_more), enabled = !busy, onClick = onMore)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Box(Modifier.weight(1f)) { if (reply.hasPrev) QuietLink(stringResource(R.string.hours_prev), enabled = !busy, onClick = onPrev) }
                Box(Modifier.weight(1f)) { if (reply.hasNext) QuietLink(stringResource(R.string.hours_next), enabled = !busy, onClick = onNext) }
            }
        }
        QuietLink(stringResource(R.string.back), enabled = true, onClick = onBack)
    }
}
