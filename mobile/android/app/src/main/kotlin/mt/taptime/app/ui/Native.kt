package mt.taptime.app.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.slideInVertically
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.snapping.rememberSnapFlingBehavior
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The staff app's native shell (the user's design of 2026-11-24): a tab bar, rounded surfaces, a
// bottom sheet, a wheel picker. Palette tokens only (skill tappa-brand); the tap and result screens
// keep Components.kt's own look -- this file draws none of them.

fun text(size: TextUnit, weight: FontWeight = FontWeight.Normal, color: Color = Palette.Ink, mono: Boolean = false, align: TextAlign = TextAlign.Start) =
    TextStyle(fontFamily = if (mono) MonoFamily else GroteskFamily, fontSize = size, fontWeight = weight, color = color,
        textAlign = align, lineHeight = (size.value * 1.3f).sp)

val Muted = Palette.Ink.copy(alpha = 0.7f)

// --- icons: 24-unit stroke paths (no icon library; the app adds no dependency) ---------------

object Icons {
    const val HOME = "M4 11l8-7 8 7v9H4z"
    const val CLOCK = "M12 3.5a8.5 8.5 0 1 1 0 17a8.5 8.5 0 1 1 0-17M12 7.5V12l3 2"
    const val GEAR = "M12 9a3 3 0 1 1 0 6a3 3 0 1 1 0-6M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1"
    const val PHONE = "M8 3h8a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2M21 9c1.2 1.6 1.2 4.4 0 6"
    const val CHEVRON_RIGHT = "M9 5l7 7-7 7"
    const val CHEVRON_LEFT = "M15 5l-7 7 7 7"
    const val BELL = "M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15zM10 20a2 2 0 0 0 4 0"
    const val LOCK = "M7 11V8a5 5 0 0 1 10 0v3M5 11h14v10H5z"
    const val GLOBE = "M12 3.5a8.5 8.5 0 1 1 0 17a8.5 8.5 0 1 1 0-17M3.5 12h17M12 3.5c2.5 2.5 2.5 14.5 0 17M12 3.5c-2.5 2.5-2.5 14.5 0 17"
    const val CHECK = "M5 12l5 5 9-10"
    const val LOGOUT = "M14 4h5v16h-5M10 8l-4 4 4 4M6 12h10"
    const val TRASH = "M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6"
    const val SHIELD = "M12 3l7 3v5c0 4.5-3 8.5-7 10c-4-1.5-7-5.5-7-10V6z"
}

@Composable
fun Icon(path: String, tint: Color = Palette.Ink, size: Dp = 22.dp, stroke: Float = 2f) {
    val p = remember(path) { PathParser().parsePathString(path).toPath() }
    Canvas(Modifier.size(size)) {
        scale(this.size.width / 24f, pivot = androidx.compose.ui.geometry.Offset.Zero) {
            drawPath(p, tint, style = Stroke(width = stroke, cap = StrokeCap.Round, join = StrokeJoin.Round))
        }
    }
}

// --- surfaces ------------------------------------------------------------------------------

@Composable
fun Surface(
    modifier: Modifier = Modifier, color: Color = Palette.Paper, radius: Dp = 22.dp, raised: Boolean = true,
    onClick: (() -> Unit)? = null, label: String? = null, content: @Composable ColumnScope.() -> Unit,
) {
    val shape = RoundedCornerShape(radius)
    var m = modifier.fillMaxWidth()
    if (raised) m = m.shadow(1.dp, shape, ambientColor = Palette.Ink.copy(alpha = 0.12f), spotColor = Palette.Ink.copy(alpha = 0.12f))
    m = m.clip(shape).background(color)
    if (onClick != null) m = m.clickable(role = Role.Button, onClick = onClick).semantics { if (label != null) contentDescription = label }
    Column(m, content = content)
}

@Composable
fun Avatar(initials: String, size: Dp = 44.dp) {
    Box(Modifier.size(size).clip(CircleShape).background(Palette.TappaGreen), contentAlignment = Alignment.Center) {
        BasicText(initials, style = text((size.value * 0.36f).sp, FontWeight.Bold, Palette.Paper))
    }
}

fun initialsOf(name: String): String =
    name.trim().split(Regex("\\s+")).filter { it.isNotEmpty() }.let { w ->
        ((w.firstOrNull()?.take(1) ?: "") + (if (w.size > 1) w.last().take(1) else "")).uppercase(java.util.Locale.ROOT)
    }

@Composable
fun IconTile(path: String, tint: Color = Palette.TappaGreen, fill: Color = Palette.GreenLite) {
    Box(Modifier.size(38.dp).clip(RoundedCornerShape(12.dp)).background(fill), contentAlignment = Alignment.Center) {
        Icon(path, tint, 20.dp)
    }
}

/** A large page title (the staff tabs). */
@Composable
fun LargeTitle(t: String) {
    BasicText(t, Modifier.padding(horizontal = 4.dp).semantics { heading() }, style = text(32.sp, FontWeight.Bold).copy(letterSpacing = (-0.8).sp))
}

/** A grouped list's section label. */
@Composable
fun SectionLabel(t: String) {
    BasicText(t, Modifier.padding(start = 8.dp, top = 6.dp), style = text(13.sp, FontWeight.SemiBold, Muted))
}

/** One row of a grouped list: an icon tile, the words, and an end slot. */
@Composable
fun ListRow(
    icon: String?, title: String, subtitle: String? = null, titleColor: Color = Palette.Ink, iconTint: Color = Palette.TappaGreen,
    iconFill: Color = Palette.GreenLite, onClick: (() -> Unit)? = null, end: @Composable () -> Unit = {},
) {
    var m = Modifier.fillMaxWidth().heightIn(min = 64.dp)
    if (onClick != null) m = m.clickable(role = Role.Button, onClick = onClick)
    Row(m.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        if (icon != null) IconTile(icon, iconTint, iconFill)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            BasicText(title, style = text(16.sp, FontWeight.Bold, titleColor))
            if (subtitle != null) BasicText(subtitle, style = text(13.sp, color = Muted))
        }
        end()
    }
}

@Composable
fun RowDivider(inset: Dp = 68.dp) {
    Box(Modifier.padding(start = inset).fillMaxWidth().height(1.dp).background(Palette.Line))
}

/** A switch: a rounded track and a knob. The whole row is the control (Role.Switch). */
@Composable
fun Toggle(on: Boolean) {
    Box(Modifier.width(52.dp).height(32.dp).clip(RoundedCornerShape(16.dp)).background(if (on) Palette.TappaGreen else Palette.Line)) {
        Box(Modifier.padding(4.dp).offset(x = if (on) 20.dp else 0.dp).size(24.dp).shadow(1.dp, CircleShape).clip(CircleShape).background(Palette.Paper))
    }
}

@Composable
fun ToggleRow(icon: String?, title: String, subtitle: String?, on: Boolean, onChange: (Boolean) -> Unit) {
    val state = if (on) "on" else "off"
    Row(
        Modifier.fillMaxWidth().heightIn(min = 64.dp).clickable(role = Role.Switch) { onChange(!on) }
            .semantics { stateDescription = state }.padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        if (icon != null) IconTile(icon)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            BasicText(title, style = text(16.sp, FontWeight.Bold))
            if (subtitle != null) BasicText(subtitle, style = text(13.sp, color = Muted))
        }
        Toggle(on)
    }
}

/** The primary action of a sheet or a card: rounded, at least 56 dp. */
@Composable
fun ActionButton(label: String, enabled: Boolean = true, color: Color = Palette.TappaGreen, onClick: () -> Unit) {
    Box(
        Modifier.fillMaxWidth().heightIn(min = 56.dp).clip(RoundedCornerShape(16.dp)).background(if (enabled) color else color.copy(alpha = 0.5f))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { BasicText(label, style = text(17.sp, FontWeight.Bold, Palette.Paper)) }
}

@Composable
fun TextAction(label: String, enabled: Boolean = true, color: Color = Muted, onClick: () -> Unit) {
    Box(Modifier.fillMaxWidth().heightIn(min = 48.dp).clip(RoundedCornerShape(12.dp)).clickable(enabled = enabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center) { BasicText(label, style = text(15.sp, FontWeight.SemiBold, color)) }
}

/** A grey placeholder that breathes while a read is in flight. */
@Composable
fun Skeleton(height: Dp, modifier: Modifier = Modifier.fillMaxWidth(), radius: Dp = 22.dp) {
    val t = rememberInfiniteTransition(label = "skeleton")
    val a by t.animateFloat(0.45f, 0.9f, infiniteRepeatable(tween(900), RepeatMode.Reverse), label = "skeleton-alpha")
    Box(modifier.height(height).alpha(a).clip(RoundedCornerShape(radius)).background(Palette.Line.copy(alpha = 0.6f)))
}

/** A short line on the tab's top: a word of what just happened. */
@Composable
fun Banner(textValue: String, tone: Tone) {
    val fill = when (tone) { Tone.OK -> Palette.GreenLite; Tone.WARN -> Palette.SaffronLite; Tone.ALERT -> Palette.Tomato.copy(alpha = 0.12f) }
    Box(Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(fill).padding(horizontal = 16.dp, vertical = 12.dp)) {
        BasicText(textValue, style = text(14.sp, FontWeight.SemiBold))
    }
}

// --- the tab shell -------------------------------------------------------------------------

enum class Tab { TODAY, HOURS, SETTINGS }

/**
 * The app's top bar, the same on every tab: the wordmark (and the business beside it), and the
 * person's initials. It stays put while the page scrolls under it; a hairline appears once it does.
 */
@Composable
fun TopBar(wordmark: String, business: String?, initials: String, scrolled: Boolean) {
    Column(Modifier.fillMaxWidth().background(Palette.Porcelain).statusBarsPadding()) {
        Row(Modifier.fillMaxWidth().height(56.dp).padding(horizontal = 20.dp), verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            BasicText(wordmark, style = text(20.sp, FontWeight.Bold, Palette.TappaGreen).copy(letterSpacing = (-0.4).sp))
            if (!business.isNullOrEmpty()) {
                Box(Modifier.width(1.dp).height(18.dp).background(Palette.Line))
                BasicText(business, Modifier.weight(1f), style = text(14.sp, FontWeight.SemiBold, Muted), maxLines = 1)
            } else Spacer(Modifier.weight(1f))
            Avatar(initials, 34.dp)
        }
        Box(Modifier.fillMaxWidth().height(1.dp).background(if (scrolled) Palette.Line else Color.Transparent))
    }
}

/** A tab's page: the top bar, a scrolling column, the tab bar. */
@Composable
fun StaffShell(
    tab: Tab, labels: Map<Tab, String>, onTab: (Tab) -> Unit, wordmark: String, business: String?, initials: String,
    overlay: @Composable BoxScope.() -> Unit = {}, content: @Composable ColumnScope.() -> Unit,
) {
    val scroll = rememberScrollState()
    Box(Modifier.fillMaxSize().background(Palette.Porcelain)) {
        Column(Modifier.fillMaxSize()) {
            TopBar(wordmark, business, initials, scroll.value > 0)
            Column(
                Modifier.weight(1f).fillMaxWidth().verticalScroll(scroll)
                    .padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
                content = content,
            )
            TabBar(tab, labels, onTab)
        }
        overlay()
    }
}

@Composable
private fun TabBar(tab: Tab, labels: Map<Tab, String>, onTab: (Tab) -> Unit) {
    Column(Modifier.fillMaxWidth().background(Palette.Paper)) {
        Box(Modifier.fillMaxWidth().height(1.dp).background(Palette.Line))
        Row(Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 8.dp, vertical = 6.dp)) {
            for (t in labels.keys) { // the tabs given, in their order (M16-33: My hours may be absent)
                val on = t == tab
                val path = when (t) { Tab.TODAY -> Icons.HOME; Tab.HOURS -> Icons.CLOCK; Tab.SETTINGS -> Icons.GEAR }
                Column(
                    Modifier.weight(1f).heightIn(min = 56.dp).clip(RoundedCornerShape(14.dp))
                        .clickable(role = Role.Tab) { onTab(t) }.semantics { selected = on }.padding(vertical = 6.dp),
                    horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Box(Modifier.width(56.dp).height(30.dp).clip(RoundedCornerShape(15.dp)).background(if (on) Palette.GreenLite else Color.Transparent),
                        contentAlignment = Alignment.Center) { Icon(path, if (on) Palette.TappaGreen else Muted) }
                    BasicText(labels[t].orEmpty(), style = text(12.sp, if (on) FontWeight.Bold else FontWeight.Medium, if (on) Palette.TappaGreen else Muted))
                }
            }
        }
    }
}

// --- overlays ------------------------------------------------------------------------------

/** A modal bottom sheet over a scrim; a tap on the scrim dismisses it (unless [busy]). */
@Composable
fun BoxScope.BottomSheet(busy: Boolean, onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    Box(Modifier.fillMaxSize().background(Palette.Ink.copy(alpha = 0.55f))
        .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, enabled = !busy, onClick = onDismiss))
    val shown = remember { MutableTransitionState(false).apply { targetState = true } }
    AnimatedVisibility(shown, Modifier.align(Alignment.BottomCenter), enter = slideInVertically { it } + fadeIn()) {
        Column(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(topStart = 28.dp, topEnd = 28.dp)).background(Palette.Paper)
                .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) {}
                .navigationBarsPadding().padding(start = 20.dp, end = 20.dp, top = 10.dp, bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Box(Modifier.align(Alignment.CenterHorizontally).width(40.dp).height(5.dp).clip(RoundedCornerShape(3.dp)).background(Palette.Line))
            content()
        }
    }
}

/** A centred confirmation over a scrim. */
@Composable
fun BoxScope.ConfirmDialog(title: String, body: String, confirm: String, cancel: String, busy: Boolean, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    Box(Modifier.fillMaxSize().background(Palette.Ink.copy(alpha = 0.55f))
        .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, enabled = !busy, onClick = onDismiss))
    Column(
        Modifier.align(Alignment.Center).padding(24.dp).fillMaxWidth().clip(RoundedCornerShape(24.dp)).background(Palette.Paper).padding(22.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        BasicText(title, style = text(20.sp, FontWeight.Bold))
        BasicText(body, style = text(15.sp, color = Palette.Ink.copy(alpha = 0.8f)))
        Spacer(Modifier.height(4.dp))
        ActionButton(confirm, enabled = !busy, color = Palette.Tomato, onClick = onConfirm)
        TextAction(cancel, enabled = !busy, onClick = onDismiss)
    }
}

/**
 * A wheel of [values]: five rows shown, the middle one chosen; a fling snaps to a row. [onPick] gets
 * the chosen index whenever the wheel comes to rest.
 */
@Composable
fun Wheel(values: List<String>, initial: Int, label: String, modifier: Modifier = Modifier, onPick: (Int) -> Unit) {
    val rowH = 44.dp
    val state = rememberLazyListState(initialFirstVisibleItemIndex = initial.coerceIn(0, values.lastIndex))
    val rowPx = with(LocalDensity.current) { rowH.toPx() }
    val centred by remember { derivedStateOf { state.firstVisibleItemIndex + if (state.firstVisibleItemScrollOffset > rowPx / 2) 1 else 0 } }
    LaunchedEffect(state) {
        snapshotFlow { state.isScrollInProgress to centred }.collect { (moving, i) -> if (!moving) onPick(i.coerceIn(0, values.lastIndex)) }
    }
    LazyColumn(
        modifier.height(rowH * 5).semantics { contentDescription = label; stateDescription = values.getOrElse(centred) { "" } },
        state = state, contentPadding = PaddingValues(vertical = rowH * 2), flingBehavior = rememberSnapFlingBehavior(state),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        items(values.size) { i ->
            val d = kotlin.math.abs(i - centred)
            Box(Modifier.height(rowH).fillMaxWidth(), contentAlignment = Alignment.Center) {
                BasicText(values[i], style = text(if (d == 0) 30.sp else 22.sp, if (d == 0) FontWeight.SemiBold else FontWeight.Normal,
                    Palette.Ink.copy(alpha = when (d) { 0 -> 1f; 1 -> 0.5f; else -> 0.25f }), mono = true))
            }
        }
    }
}
