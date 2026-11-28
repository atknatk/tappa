package mt.taptime.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import mt.taptime.app.R
import mt.taptime.app.staff.ClaimWords
import mt.taptime.app.staff.ShiftWords
import mt.taptime.app.staff.StaffFlow
import mt.taptime.app.wire.HoursRecord
import mt.taptime.app.wire.HoursReply
import mt.taptime.app.wire.MeReply
import mt.taptime.app.wire.OpenClaim
import mt.taptime.app.wire.SentClaim
import mt.taptime.app.wire.UsualShift

// The staff app's own screens (M16-44/46/47/48/33; the user's native design of 2026-11-24): three
// tabs -- Today, My hours, Settings -- and the missing-time sheet. The tap and result screens are
// untouched: clocking in and out is still the plaque's (CLAUDE.md §9).

/** What the last claim send answered, for the sheet's or the tab's line. */
enum class ClaimNotice { SENT, EXPIRED, RESOLVED, NOT_ACTIVE, STILL_OPEN, NETWORK }

@Composable
fun tabLabels() = mapOf(
    Tab.TODAY to stringResource(R.string.tab_today),
    Tab.HOURS to stringResource(R.string.tab_hours),
    Tab.SETTINGS to stringResource(R.string.tab_settings),
)

@Composable
private fun duration(minutes: Int): String = stringResource(R.string.hours_duration, minutes / 60, minutes % 60)

private fun firstName(name: String) = name.trim().split(Regex("\\s+")).firstOrNull().orEmpty()

private fun clock(ms: Long): Pair<String, String> {
    val s = (ms / 1000).coerceAtLeast(0)
    return "%d:%02d".format(s / 3600, (s / 60) % 60) to ":%02d".format(s % 60)
}

@Composable
private fun shiftLine(s: UsualShift): String = ShiftWords.line(s).let { stringResource(it.id, *it.args.toTypedArray()) }

// --- Today ---------------------------------------------------------------------------------

/**
 * Today: who, the live status (a ticking counter while clocked in, the shift's progress when the
 * owner marked shifts real), the plaque's instruction, a missing time and the week's counted total.
 * [offsetMs] is the server clock's (server = phone + offset): the counter counts server time.
 */
@Composable
fun TodayContent(
    me: MeReply, hasReader: Boolean, offsetMs: Long, week: HoursReply?, weekLoading: Boolean, notice: ClaimNotice?,
    onMissing: () -> Unit, onHours: () -> Unit,
) {
    val p = me.presence
    var now by remember { mutableLongStateOf(System.currentTimeMillis() + offsetMs) }
    LaunchedEffect(p?.inSinceMs, offsetMs) {
        while (p != null) {
            now = System.currentTimeMillis() + offsetMs
            delay(1000 - (now % 1000))
        }
    }
    LargeTitle(stringResource(R.string.today_hello, firstName(me.employeeName)))
    // The sheet closed with the list's last record: its answer is said here (iOS's Today banner).
    notice?.let { ClaimBanner(it) }

    // The status card: ink above, a paper stub with the plaque's instruction below.
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(24.dp))) {
        Column(Modifier.fillMaxWidth().background(Palette.Ink).padding(start = 22.dp, end = 22.dp, top = 22.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Box(Modifier.size(8.dp).clip(CircleShape).background(if (p != null) Palette.GreenLite else Palette.Line))
                    BasicText(stringResource(if (p != null) R.string.today_at_work else R.string.today_not_in).uppercase(LocalConfiguration.current.locales[0]),
                        style = text(12.sp, FontWeight.SemiBold, Palette.Paper, mono = true).copy(letterSpacing = 1.6.sp))
                }
                if (p != null) BasicText(stringResource(R.string.today_in_at, p.inSinceLocal), style = text(12.sp, color = Palette.Paper.copy(alpha = 0.75f), mono = true))
            }
            if (p != null) {
                val (hm, sec) = clock(now - p.inSinceMs)
                Row(verticalAlignment = Alignment.Bottom, modifier = Modifier.semantics { contentDescription = hm }) {
                    BasicText(hm, style = text(64.sp, FontWeight.Medium, Palette.Paper, mono = true).copy(letterSpacing = (-2).sp, lineHeight = 64.sp))
                    BasicText(sec, Modifier.padding(start = 4.dp, bottom = 8.dp), style = text(22.sp, color = Palette.Paper.copy(alpha = 0.6f), mono = true))
                }
                val end = p.shiftEndsAtMs
                if (end != null && end > p.inSinceMs) {
                    val frac = ((now - p.inSinceMs).toFloat() / (end - p.inSinceMs)).coerceIn(0f, 1f)
                    val leftMin = ((end - now) / 60000).toInt()
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Box(Modifier.fillMaxWidth().height(8.dp).clip(RoundedCornerShape(4.dp)).background(Palette.Paper.copy(alpha = 0.14f))) {
                            Box(Modifier.fillMaxWidth(frac).fillMaxHeight().clip(RoundedCornerShape(4.dp)).background(Palette.GreenLite))
                        }
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                            BasicText(stringResource(R.string.today_shift_until, p.shiftEndsAtLocal.orEmpty()), style = text(12.sp, color = Palette.Paper.copy(alpha = 0.75f), mono = true))
                            BasicText(
                                if (leftMin >= 0) stringResource(R.string.today_left, duration(leftMin)) else stringResource(R.string.today_over, duration(-leftMin)),
                                style = text(12.sp, color = Palette.Paper.copy(alpha = 0.75f), mono = true),
                            )
                        }
                    }
                }
            } else {
                BasicText(stringResource(R.string.today_not_in_body), style = text(22.sp, FontWeight.Bold, Palette.Paper))
                me.shift?.let { BasicText(shiftLine(it), style = text(13.sp, color = Palette.Paper.copy(alpha = 0.75f))) }
            }
        }
        Row(Modifier.fillMaxWidth().background(Palette.Paper).padding(horizontal = 22.dp, vertical = 18.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            IconTile(Icons.PHONE)
            Column(verticalArrangement = Arrangement.spacedBy(1.dp)) {
                if (hasReader) {
                    BasicText(stringResource(if (p != null) R.string.today_tap_out else R.string.today_tap_in), style = text(16.sp, FontWeight.Bold))
                    BasicText(stringResource(R.string.today_tap_hint), style = text(13.sp, color = Muted))
                } else {
                    BasicText(stringResource(R.string.p_no_nfc_title), style = text(16.sp, FontWeight.Bold))
                    BasicText(stringResource(R.string.p_no_nfc_hint), style = text(13.sp, color = Muted))
                }
            }
        }
    }

    if (me.missingTimes > 0) {
        Surface(color = Palette.SaffronLite, radius = 18.dp, raised = false, onClick = onMissing) {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                Box(Modifier.size(40.dp).clip(RoundedCornerShape(12.dp)).background(Palette.Saffron), contentAlignment = Alignment.Center) {
                    BasicText(me.missingTimes.toString(), style = text(16.sp, FontWeight.SemiBold, mono = true))
                }
                Column(Modifier.weight(1f)) {
                    BasicText(if (me.missingTimes == 1) stringResource(R.string.today_missing_one) else stringResource(R.string.today_missing_many, me.missingTimes),
                        style = text(15.sp, FontWeight.Bold))
                    BasicText(stringResource(R.string.today_missing_sub), style = text(13.sp, color = Palette.Ink.copy(alpha = 0.75f)))
                }
                Icon(Icons.CHEVRON_RIGHT, size = 18.dp)
            }
        }
    }

    if (me.hoursWeeks > 0) {
        if (week == null && weekLoading) Skeleton(76.dp)
        else if (week != null) {
            Surface(radius = 18.dp, onClick = onHours) {
                Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        BasicText(stringResource(R.string.today_week), style = text(13.sp, color = Muted))
                        BasicText(duration(week.totals.workedMinutes), style = text(24.sp, FontWeight.Medium, mono = true))
                    }
                    WeekBars(week)
                    Icon(Icons.CHEVRON_RIGHT, size = 18.dp)
                }
            }
        }
    }
}

@Composable
private fun WeekBars(week: HoursReply) {
    val max = (week.days.maxOfOrNull { it.workedMinutes } ?: 0).coerceAtLeast(1)
    Row(Modifier.height(36.dp), verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        for (d in week.days) {
            val h = if (d.workedMinutes > 0) (6 + 30f * d.workedMinutes / max).dp else 4.dp
            Box(Modifier.width(8.dp).height(h).clip(RoundedCornerShape(3.dp)).background(if (d.workedMinutes > 0) Palette.TappaGreen else Palette.Line))
        }
    }
}

// --- the missing-time sheet ----------------------------------------------------------------

private val hourValues = (0..23).map { "%02d".format(it) }
private val minuteValues = (0..59).map { "%02d".format(it) }

private fun parseHM(v: String?): Pair<Int, Int>? {
    val parts = v?.split(":") ?: return null
    if (parts.size != 2) return null
    val h = parts[0].toIntOrNull() ?: return null
    val m = parts[1].toIntOrNull() ?: return null
    return if (h in 0..23 && m in 0..59) h to m else null
}

/**
 * A claim's answer as one banner: the title, " — ", then the card's lines (M16-44: the browser's,
 * without "Your tap itself is recorded." -- there is no tap on this screen). iOS says the same
 * (StaffCopy.expired / resolved / notActive / stillOpen).
 */
@Composable
private fun ClaimBanner(notice: ClaimNotice) {
    val tell = stringResource(R.string.claim_tell_manager)
    when (notice) {
        ClaimNotice.SENT -> Banner(stringResource(R.string.claim_sent), Tone.OK)
        ClaimNotice.EXPIRED -> Banner(stringResource(R.string.claim_expired_title) + " — " + stringResource(R.string.claim_expired_message) + " " + tell, Tone.ALERT)
        ClaimNotice.RESOLVED -> Banner(stringResource(R.string.claim_resolved_title) + " — " + stringResource(R.string.claim_resolved_message) + " " +
            stringResource(R.string.claim_resolved_hint), Tone.WARN)
        ClaimNotice.NOT_ACTIVE -> Banner(stringResource(R.string.claim_not_active_title) + " — " + stringResource(R.string.claim_not_active_message) + " " + tell, Tone.ALERT)
        ClaimNotice.STILL_OPEN -> Banner(stringResource(R.string.claim_still_open_title) + " — " + stringResource(R.string.claim_still_open_message), Tone.WARN)
        ClaimNotice.NETWORK -> Banner(stringResource(R.string.p_network_title), Tone.ALERT)
    }
}

/**
 * One record that takes a claim: the record, the question, a wheel for the time, send or skip --
 * and under it the person's own [sent] claims (M16-44: waiting, approved, approved at the manager's
 * clock, not approved).
 */
@Composable
fun BoxScope.ClaimSheet(
    c: OpenClaim, index: Int, count: Int, sent: List<SentClaim>, notice: ClaimNotice?, problem: String?, busy: Boolean,
    onSend: (String) -> Unit, onSkip: () -> Unit, onDismiss: () -> Unit,
) {
    val locale = LocalConfiguration.current.locales[0]
    val start = parseHM(c.prefill) ?: parseHM(c.anchorAt) ?: (9 to 0)
    var hour by remember(c.context) { mutableIntStateOf(start.first) }
    var minute by remember(c.context) { mutableIntStateOf(start.second) }
    // The time last sent: the server's "does not fit" is about THAT time, so it stands only while
    // the wheels still show it; before a send, and once they move, the hint just names the window.
    var sentAt by remember(c.context) { mutableStateOf<String?>(null) }
    val picked = "%02d:%02d".format(hour, minute)
    val refusal = problem?.takeIf { sentAt == picked }
    BottomSheet(busy, onDismiss) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.clip(RoundedCornerShape(8.dp)).background(Palette.SaffronLite).padding(horizontal = 10.dp, vertical = 4.dp)) {
                    BasicText(ClaimWords.date(c.anchorDate, locale) + " · " + c.anchorAt, style = text(12.sp, FontWeight.SemiBold, mono = true))
                }
                if (count > 1) BasicText("${index + 1}/$count", style = text(12.sp, color = Muted, mono = true))
            }
            val lead = ClaimWords.lead(c.kind, c.day, c.anchorDate, c.anchorAt, locale)
            BasicText(stringResource(lead.id, *lead.args.toTypedArray()), style = text(15.sp, color = Palette.Ink.copy(alpha = 0.8f)))
            BasicText(stringResource(ClaimWords.question(c.kind)), style = text(26.sp, FontWeight.Bold).copy(letterSpacing = (-0.5).sp))
        }
        Box(Modifier.fillMaxWidth().clip(RoundedCornerShape(20.dp)).background(Palette.Porcelain)) {
            Box(Modifier.align(Alignment.Center).padding(horizontal = 12.dp).fillMaxWidth().height(48.dp).clip(RoundedCornerShape(14.dp)).background(Palette.Paper))
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Wheel(hourValues, start.first, stringResource(R.string.sheet_hour), Modifier.weight(1f)) { hour = it }
                BasicText(":", style = text(30.sp, FontWeight.SemiBold, mono = true))
                Wheel(minuteValues, start.second, stringResource(R.string.sheet_minute), Modifier.weight(1f)) { minute = it }
            }
        }
        BasicText(
            refusal ?: stringResource(R.string.claim_window_hint, stringResource(ClaimWords.window(c.fromInclusive, c.toInclusive), c.windowFrom, c.windowTo)),
            Modifier.fillMaxWidth(), style = text(13.sp, if (refusal != null) FontWeight.SemiBold else FontWeight.Normal,
                if (refusal != null) Palette.Tomato else Muted, align = TextAlign.Center),
        )
        if (notice != null && notice != ClaimNotice.SENT) ClaimBanner(notice)
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            ActionButton(stringResource(if (busy) R.string.sheet_sending else R.string.claim_send), enabled = !busy) {
                sentAt = picked
                onSend(picked)
            }
            // The browser card's "You can skip this." (claim.templ), here the control that moves on.
            TextAction(stringResource(R.string.claim_skip), enabled = !busy, onClick = onSkip)
        }
        if (sent.isNotEmpty()) {
            Column(Modifier.fillMaxWidth().heightIn(max = 180.dp).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(10.dp)) {
                BasicText(stringResource(R.string.missing_sent_label), style = text(13.sp, FontWeight.SemiBold, Muted))
                for (s in sent) {
                    val kind = stringResource(if (s.kind == "in") R.string.missing_kind_in else R.string.missing_kind_out)
                    val state = ClaimWords.state(s.state, s.approvedAt)
                    Column(Modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        BasicText(stringResource(R.string.missing_sent_row, ClaimWords.date(s.anchorDate, locale), kind, s.claimedAt),
                            style = text(14.sp, mono = true))
                        BasicText(stringResource(state.id, *state.args.toTypedArray()), style = text(13.sp, color = Muted))
                    }
                }
            }
        }
    }
}

// --- My hours ------------------------------------------------------------------------------

private fun weekLabel(week: String, locale: java.util.Locale): String = try {
    val d = java.time.LocalDate.parse(week)
    val e = d.plusDays(6)
    val fmt = java.time.format.DateTimeFormatter.ofPattern("d MMM", locale)
    if (d.month == e.month) "${d.dayOfMonth}–${e.format(fmt)}" else "${d.format(fmt)} – ${e.format(fmt)}"
} catch (_: java.time.format.DateTimeParseException) { week }

private fun weekday(date: String, locale: java.util.Locale, pattern: String): String = try {
    java.time.LocalDate.parse(date).format(java.time.format.DateTimeFormatter.ofPattern(pattern, locale))
} catch (_: java.time.format.DateTimeParseException) { date }

@Composable
private fun stateWord(state: String): String = stringResource(
    when (state) {
        "approved" -> R.string.hours_state_approved
        "waiting" -> R.string.hours_state_waiting
        "repeat" -> R.string.hours_state_repeat
        else -> R.string.hours_state_rejected
    },
)

private fun toneOf(state: String): Tone? = when (state) {
    "approved" -> Tone.OK
    "waiting" -> Tone.WARN
    "rejected" -> Tone.ALERT
    else -> null
}

/** My hours: the week switcher, the counted total, the seven days and the chosen day's timeline. */
@Composable
fun HoursContent(
    hoursWeeks: Int, reply: HoursReply?, records: List<HoursRecord>, busy: Boolean, network: Boolean,
    onPrev: () -> Unit, onNext: () -> Unit, onMore: () -> Unit, onRetry: () -> Unit,
) {
    val locale = LocalConfiguration.current.locales[0]
    val res = LocalContext.current.resources
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
        LargeTitle(stringResource(R.string.tab_hours))
        if (reply != null) {
            Row(Modifier.clip(RoundedCornerShape(14.dp)).background(Palette.Paper).padding(2.dp), verticalAlignment = Alignment.CenterVertically) {
                WeekArrow(Icons.CHEVRON_LEFT, stringResource(R.string.hours_prev), reply.hasPrev && !busy, onPrev)
                BasicText(weekLabel(reply.week, locale), Modifier.padding(horizontal = 4.dp), style = text(13.sp, FontWeight.SemiBold, mono = true))
                WeekArrow(Icons.CHEVRON_RIGHT, stringResource(R.string.hours_next), reply.hasNext && !busy, onNext)
            }
        }
    }
    if (hoursWeeks <= 0) {
        Surface(radius = 22.dp) {
            Column(Modifier.padding(22.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                IconTile(Icons.CLOCK)
                BasicText(stringResource(R.string.hours_off_title), style = text(18.sp, FontWeight.Bold))
                BasicText(stringResource(R.string.hours_off_body), style = text(14.sp, color = Muted))
            }
        }
        return
    }
    if (network && reply == null) {
        Banner(stringResource(R.string.p_network_title), Tone.ALERT)
        TextAction(stringResource(R.string.try_again), onClick = onRetry)
        return
    }
    if (reply == null) {
        Skeleton(132.dp); Skeleton(80.dp); Skeleton(220.dp)
        return
    }
    if (network) Banner(stringResource(R.string.p_network_title), Tone.ALERT)
    val t = reply.totals
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(24.dp)).background(Palette.Ink).padding(horizontal = 22.dp, vertical = 20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.SpaceBetween) {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                BasicText(stringResource(if (reply.hasNext) R.string.hours_counted_label else R.string.today_week), style = text(13.sp, color = Palette.Paper.copy(alpha = 0.75f)))
                BasicText(duration(t.workedMinutes), style = text(36.sp, FontWeight.Medium, Palette.Paper, mono = true).copy(letterSpacing = (-1).sp))
            }
            if (t.openEntries > 0) Box(Modifier.clip(RoundedCornerShape(10.dp)).background(Palette.Saffron.copy(alpha = 0.25f)).padding(horizontal = 10.dp, vertical = 6.dp)) {
                BasicText(stringResource(R.string.hours_chip_open, t.openEntries), style = text(12.sp, FontWeight.SemiBold, Palette.SaffronLite))
            }
        }
        val notes = buildList {
            if (t.awaitingMinutes > 0) add(res.getString(R.string.hours_awaiting, res.getString(R.string.hours_duration, t.awaitingMinutes / 60, t.awaitingMinutes % 60)))
            if (t.openEntries > 0) add(res.getQuantityString(R.plurals.hours_open, t.openEntries, t.openEntries))
            if (t.checkInMissing > 0) add(res.getQuantityString(R.plurals.hours_in_missing, t.checkInMissing, t.checkInMissing))
            if (reply.waitingRecords + reply.waitingClaims > 0) {
                val parts = buildList {
                    if (reply.waitingRecords > 0) add(res.getQuantityString(R.plurals.hours_waiting_records, reply.waitingRecords, reply.waitingRecords))
                    if (reply.waitingClaims > 0) add(res.getQuantityString(R.plurals.hours_waiting_claims, reply.waitingClaims, reply.waitingClaims))
                }
                add(res.getString(R.string.hours_waiting_line, parts.joinToString(" · ")))
            }
            if (t.truncated) add(res.getString(R.string.hours_truncated))
        }
        if (notes.isNotEmpty()) BasicText(notes.joinToString(" "), style = text(13.sp, color = Palette.Paper.copy(alpha = 0.78f)))
    }

    // Seven day chips; the chosen day's records below. Today (or the week's last day with a
    // record) is chosen first.
    val byDay = records.groupBy { it.date }
    val today = java.time.LocalDate.now().toString()
    var chosen by remember(reply.week) {
        mutableStateOf(reply.days.firstOrNull { it.date == today }?.date ?: reply.days.lastOrNull { byDay.containsKey(it.date) }?.date ?: reply.days.firstOrNull()?.date)
    }
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        for (d in reply.days) {
            val on = d.date == chosen
            val recs = byDay[d.date].orEmpty()
            val open = recs.isNotEmpty() && d.workedMinutes == 0 && recs.any { it.direction == "in" }
            val future = d.date > today
            Column(
                Modifier.weight(1f).clip(RoundedCornerShape(14.dp))
                    .background(if (on) Palette.Ink else if (future) Palette.Paper.copy(alpha = 0.5f) else Palette.Paper)
                    .then(if (open && !on) Modifier.border(2.dp, Palette.Saffron, RoundedCornerShape(14.dp)) else Modifier)
                    .clickable(enabled = !future, role = Role.Tab) { chosen = d.date }.semantics { selected = on }
                    .padding(top = 10.dp, bottom = 8.dp),
                horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(5.dp),
            ) {
                val fg = if (on) Palette.Paper else if (future) Palette.Ink.copy(alpha = 0.5f) else Palette.Ink
                BasicText(weekday(d.date, locale, "EEE"), style = text(11.sp, color = fg.copy(alpha = if (on) 0.75f else 0.7f)))
                BasicText(weekday(d.date, locale, "d"), style = text(15.sp, FontWeight.SemiBold, fg, mono = true))
                Box(Modifier.width(18.dp).height(4.dp).clip(RoundedCornerShape(2.dp)).background(
                    when { d.workedMinutes > 0 -> if (on) Palette.GreenLite else Palette.TappaGreen; open -> Palette.Saffron; future -> Palette.Paper.copy(alpha = 0f); else -> Palette.Line }))
                BasicText(if (d.workedMinutes > 0) "%d:%02d".format(d.workedMinutes / 60, d.workedMinutes % 60) else if (open) "•" else "—",
                    style = text(10.sp, color = fg, mono = true))
            }
        }
    }

    val day = reply.days.firstOrNull { it.date == chosen }
    if (day != null) {
        val recs = byDay[day.date].orEmpty()
        DayTimeline(day.date, day.workedMinutes, recs, StaffFlow.saysNoRecords(recs.size, reply), locale)
    }
    if (reply.nextCursor != null) TextAction(stringResource(R.string.hours_more), enabled = !busy, onClick = onMore)
}

@Composable
private fun WeekArrow(path: String, label: String, enabled: Boolean, onClick: () -> Unit) {
    Box(Modifier.size(40.dp).clip(RoundedCornerShape(12.dp)).clickable(enabled = enabled, role = Role.Button, onClick = onClick)
        .semantics { contentDescription = label }, contentAlignment = Alignment.Center) {
        Icon(path, if (enabled) Palette.Ink else Palette.Ink.copy(alpha = 0.25f), 18.dp, 2.2f)
    }
}

@Composable
private fun DayTimeline(date: String, worked: Int, recs: List<HoursRecord>, noRecords: Boolean, locale: java.util.Locale) {
    Surface(radius = 24.dp) {
        Column(Modifier.padding(horizontal = 20.dp, vertical = 18.dp)) {
            Row(Modifier.fillMaxWidth().padding(bottom = 12.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                BasicText(weekday(date, locale, "EEEE, d MMMM"), style = text(17.sp, FontWeight.Bold))
                if (worked > 0) BasicText(duration(worked), style = text(14.sp, FontWeight.SemiBold, mono = true))
            }
            if (recs.isEmpty()) {
                if (noRecords) BasicText(stringResource(R.string.hours_day_empty), style = text(14.sp, color = Muted))
                return@Column
            }
            val directed = recs.filter { it.direction.isNotEmpty() }
            directed.forEachIndexed { i, r ->
                TimelineRow(r)
                if (i < directed.lastIndex) {
                    val next = directed[i + 1]
                    val pair = r.direction == "in" && next.direction == "out"
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.width(24.dp), contentAlignment = Alignment.Center) {
                            Box(Modifier.width(2.dp).height(40.dp).background(if (pair) Palette.TappaGreen else Palette.Line))
                        }
                        Spacer(Modifier.width(12.dp))
                        if (pair) {
                            val mins = minutesBetween(r.at, next.at, next.date != r.date)
                            if (mins != null) Box(Modifier.clip(RoundedCornerShape(8.dp)).background(Palette.GreenLite).padding(horizontal = 9.dp, vertical = 3.dp)) {
                                BasicText(duration(mins), style = text(12.sp, FontWeight.SemiBold, Palette.TappaGreen, mono = true))
                            }
                        }
                    }
                }
            }
            val others = recs.filter { it.direction.isEmpty() }
            if (others.isNotEmpty()) {
                Box(Modifier.padding(vertical = 12.dp).fillMaxWidth().height(1.dp).background(Palette.Line))
                for (r in others) BasicText("${r.at} · ${stateWord(r.state)}", style = text(13.sp, color = Muted))
            }
        }
    }
}

private fun minutesBetween(a: String, b: String, nextDay: Boolean): Int? {
    val (ah, am) = parseHM(a) ?: return null
    val (bh, bm) = parseHM(b) ?: return null
    val m = (bh * 60 + bm) - (ah * 60 + am) + if (nextDay) 24 * 60 else 0
    return m.takeIf { it >= 0 }
}

@Composable
private fun TimelineRow(r: HoursRecord) {
    val tone = toneOf(r.state)
    Row(Modifier.fillMaxWidth().heightIn(min = 44.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.width(24.dp), contentAlignment = Alignment.Center) {
            Box(Modifier.size(22.dp).clip(CircleShape).background((tone?.color ?: Palette.Line).copy(alpha = 0.18f)), contentAlignment = Alignment.Center) {
                Box(Modifier.size(12.dp).clip(CircleShape).background(tone?.color ?: Palette.Line))
            }
        }
        Column(Modifier.weight(1f)) {
            BasicText(stringResource(if (r.direction == "in") R.string.hours_dir_in else R.string.hours_dir_out), style = text(15.sp, FontWeight.Bold))
            val sub = buildList {
                if (r.venue.isNotEmpty()) add(r.venue)
                when (r.channel) {
                    "manual" -> add(stringResource(R.string.hours_channel_manual))
                    "qr" -> add(stringResource(R.string.hours_channel_qr))
                    "qr_live" -> add(stringResource(R.string.hours_channel_qr_live))
                }
                if (r.state != "approved") add(stateWord(r.state))
                when (r.claim) {
                    "pending" -> add(stringResource(R.string.hours_claim_pending))
                    "approved" -> add(stringResource(R.string.hours_claim_approved))
                    "rejected" -> add(stringResource(R.string.hours_claim_rejected))
                }
            }
            if (sub.isNotEmpty()) BasicText(sub.joinToString(" · "), style = text(13.sp, color = Muted))
        }
        BasicText(r.at, style = text(18.sp, FontWeight.Medium, mono = true))
    }
}

// --- Settings ------------------------------------------------------------------------------

/**
 * Settings: the person, the two switches, the usual shift, the language, this phone's sign-out,
 * the account deletion request and the privacy notice (M16-38; App Store 5.1.1(i)).
 */
@Composable
fun SettingsContent(
    me: MeReply, remind: Boolean, timer: Boolean, notificationsOff: Boolean, version: String, canPickLanguage: Boolean,
    onRemind: (Boolean) -> Unit, onTimer: (Boolean) -> Unit, onLanguage: () -> Unit, onSignOut: () -> Unit, onDelete: () -> Unit,
    onPrivacy: () -> Unit,
) {
    LargeTitle(stringResource(R.string.tab_settings))
    Surface(radius = 22.dp) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            Avatar(initialsOf(me.employeeName), 56.dp)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                BasicText(me.employeeName, style = text(18.sp, FontWeight.Bold))
                me.businessName.ifEmpty { me.brand.name.orEmpty() }.takeIf { it.isNotEmpty() }?.let { BasicText(it, style = text(13.sp, color = Muted)) }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Icon(Icons.CHECK, Palette.TappaGreen, 14.dp, 2.4f)
                    BasicText(stringResource(R.string.settings_connected), style = text(12.sp, FontWeight.SemiBold, Palette.TappaGreen))
                }
            }
        }
    }

    SectionLabel(stringResource(R.string.settings_notifications))
    Surface(radius = 22.dp) {
        ToggleRow(Icons.BELL, stringResource(R.string.settings_remind), stringResource(R.string.settings_remind_hint), remind, onRemind)
        RowDivider()
        ToggleRow(Icons.LOCK, stringResource(R.string.settings_timer), null, timer, onTimer)
    }
    if (notificationsOff) Banner(stringResource(R.string.settings_notifications_off), Tone.WARN)

    // M16-48: only while the owner marked shifts real (`shift` set) -- whatever My hours says.
    me.shift?.let { s ->
        SectionLabel(stringResource(R.string.settings_shift_label))
        Surface(radius = 22.dp) {
            ListRow(Icons.CLOCK, stringResource(R.string.settings_usual), ShiftWords.venue(s)) {
                Column(horizontalAlignment = Alignment.End) {
                    BasicText(ShiftWords.clocks(s), style = text(15.sp, FontWeight.SemiBold, mono = true))
                    ShiftWords.nextDay(s)?.let { BasicText(stringResource(it), style = text(12.sp, color = Muted)) }
                }
            }
        }
    }

    SectionLabel(stringResource(R.string.settings_account))
    Surface(radius = 22.dp) {
        if (canPickLanguage) {
            ListRow(Icons.GLOBE, stringResource(R.string.settings_language), stringResource(R.string.settings_language_sub), onClick = onLanguage) {
                Icon(Icons.CHEVRON_RIGHT, size = 18.dp)
            }
            RowDivider()
        }
        ListRow(Icons.LOGOUT, stringResource(R.string.settings_signout), null, titleColor = Palette.Tomato, iconTint = Palette.Tomato,
            iconFill = Palette.Tomato.copy(alpha = 0.1f), onClick = onSignOut)
        RowDivider()
        ListRow(Icons.TRASH, stringResource(R.string.settings_delete), stringResource(R.string.settings_delete_sub), titleColor = Palette.Tomato,
            iconTint = Palette.Tomato, iconFill = Palette.Tomato.copy(alpha = 0.1f), onClick = onDelete)
    }
    Surface(radius = 22.dp) {
        ListRow(Icons.SHIELD, stringResource(R.string.settings_privacy_policy), null, onClick = onPrivacy) {
            Icon(Icons.CHEVRON_RIGHT, size = 18.dp)
        }
    }
    BasicText(stringResource(R.string.settings_privacy), Modifier.padding(horizontal = 8.dp), style = text(12.sp, color = Muted))
    BasicText(stringResource(R.string.settings_version, version), Modifier.padding(horizontal = 8.dp), style = text(12.sp, color = Muted, mono = true))
}
