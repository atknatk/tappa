package mt.taptime.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import mt.taptime.app.activation.AccentFill

// The browser wizard's components, drawn by hand in Compose foundation (Q36: no material3;
// skill tappa-brand). Palette tokens verbatim; no other colour except the tenant accent, and
// that only on the wizard's primary button (ADR 0023 §2, WL-13). The fonts are the platform's
// (sans for Space Grotesk's place, monospace for IBM Plex Mono's): the web's self-hosted woff2
// files are not an Android font format, and bundling the families is a decision of its own.
object Palette {
    val Ink = Color(0xFF152219)
    val Porcelain = Color(0xFFEDF0EA)
    val Paper = Color(0xFFFFFDF4)
    val TappaGreen = Color(0xFF1F5C41)
    val Saffron = Color(0xFFD98E2B)
    val Tomato = Color(0xFFBE3D2A)
    val Line = Color(0xFFC9D2C8)

    fun rgb(v: Int) = Color(0xFF000000 or v.toLong())
}

enum class Tone(val color: Color) { OK(Palette.TappaGreen), WARN(Palette.Saffron), ALERT(Palette.Tomato) }

private val Display = TextStyle(color = Palette.Ink, fontFamily = FontFamily.SansSerif)
private val Mono = TextStyle(color = Palette.Ink, fontFamily = FontFamily.Monospace)

@Composable
fun Page(header: @Composable () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Palette.Porcelain)
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        header()
        content()
    }
}

/** The header without a logo: green "taptime" for Taptime's look, ink beside a tenant accent (K-2a). */
@Composable
fun Wordmark(word: String, tagline: String, inkWord: Boolean) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.Bottom) {
        BasicText(word, style = Display.copy(fontSize = 18.sp, fontWeight = FontWeight.Bold,
            color = if (inkWord) Palette.Ink else Palette.TappaGreen))
        BasicText(tagline.uppercase(), style = Mono.copy(fontSize = 10.sp, color = Palette.Ink.copy(alpha = 0.7f), letterSpacing = 1.5.sp))
    }
}

@Composable
fun DocketLabel(text: String) {
    BasicText(text.uppercase(LocalConfiguration.current.locales[0]), style = Mono.copy(fontSize = 11.sp, fontWeight = FontWeight.Bold,
        color = Palette.Ink.copy(alpha = 0.7f), letterSpacing = 1.5.sp))
}

@Composable
fun Heading(text: String, muted: Boolean = false) {
    BasicText(text, modifier = Modifier.semantics { heading() },
        style = Display.copy(fontSize = 28.sp, lineHeight = 32.sp, fontWeight = FontWeight.Bold,
            color = if (muted) Palette.Ink.copy(alpha = 0.7f) else Palette.Ink))
}

@Composable
fun Body(text: String, bold: Boolean = false, muted: Boolean = false, small: Boolean = false, center: Boolean = false) {
    BasicText(text, modifier = if (center) Modifier.fillMaxWidth() else Modifier,
        style = Display.copy(
            fontSize = if (small) 14.sp else 16.sp, lineHeight = if (small) 20.sp else 22.sp,
            fontWeight = if (bold) FontWeight.Bold else FontWeight.Normal,
            color = Palette.Ink.copy(alpha = if (muted) 0.7f else 0.85f),
            textAlign = if (center) TextAlign.Center else TextAlign.Start,
        ))
}

/** The wizard panel; [docket] draws the paper slip with its line edge. */
@Composable
fun Panel(docket: Boolean = false, content: @Composable ColumnScope.() -> Unit) {
    val m = if (docket) Modifier.fillMaxWidth().background(Palette.Paper).border(1.dp, Palette.Line).padding(20.dp)
    else Modifier.fillMaxWidth()
    Column(m, verticalArrangement = Arrangement.spacedBy(8.dp), content = content)
}

/** The Notice component: the tone on the left edge, ink words on paper. */
@Composable
fun Notice(tone: Tone, title: String?, vararg lines: String) {
    Row(Modifier.fillMaxWidth().background(Palette.Paper)) {
        Box(Modifier.width(4.dp).heightIn(min = 48.dp).background(tone.color))
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            if (title != null) BasicText(title, style = Display.copy(fontSize = 16.sp, fontWeight = FontWeight.Bold))
            for (l in lines) Body(l, small = true)
        }
    }
}

/**
 * The tap-button: at least 64 dp, square corners. [accent] is the tenant's fill (the wizard's
 * steps 1-2, WL-13), its label paper or ink and its 2 dp ink edge as AccentFill decides.
 */
@Composable
fun PrimaryButton(text: String, accent: AccentFill?, enabled: Boolean, onClick: () -> Unit) {
    val fill = accent?.let { Palette.rgb(it.fill) } ?: Palette.TappaGreen
    val label = accent?.let { Palette.rgb(it.label) } ?: Palette.Paper
    var m = Modifier.fillMaxWidth().heightIn(min = 64.dp).background(if (enabled) fill else fill.copy(alpha = 0.5f))
    if (accent?.edge == true) m = m.border(2.dp, Palette.Ink)
    Box(m.clickable(enabled = enabled, role = Role.Button, onClick = onClick), contentAlignment = Alignment.Center) {
        BasicText(text, style = Display.copy(fontSize = 20.sp, fontWeight = FontWeight.Bold, color = label))
    }
}

/** The quiet link under a step ("Back"), a 48 dp touch target. */
@Composable
fun QuietLink(text: String, enabled: Boolean = true, onClick: () -> Unit) {
    Box(Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable(enabled = enabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center) {
        BasicText(text, style = Display.copy(fontSize = 16.sp, fontWeight = FontWeight.Bold, color = Palette.Ink.copy(alpha = 0.7f)))
    }
}

/**
 * The tap screen's "Not right? …" (tap.templ's tapCorrection): ink on porcelain, underlined, at
 * least 44 dp tall, 24 dp under the primary button (the caller's spacing plus this 8 dp top margin).
 * It takes no accent: the accent's slot is the primary button's fill and nothing else (ADR 0023 §2).
 */
@Composable
fun CorrectionLink(text: String, enabled: Boolean, onClick: () -> Unit) {
    Box(Modifier.fillMaxWidth().padding(top = 8.dp), contentAlignment = Alignment.Center) {
        Box(Modifier.heightIn(min = 44.dp).clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 12.dp), contentAlignment = Alignment.Center) {
            BasicText(text, style = Display.copy(fontSize = 16.sp, textDecoration = TextDecoration.Underline,
                color = if (enabled) Palette.Ink else Palette.Ink.copy(alpha = 0.5f)))
        }
    }
}

/** The manager-approval pairing code (M16-23): mono, large, already grouped ("482 913"), centred. */
@Composable
fun PairingCodeText(grouped: String) {
    Box(Modifier.fillMaxWidth().background(Palette.Paper).border(1.dp, Palette.Line).padding(vertical = 16.dp),
        contentAlignment = Alignment.Center) {
        BasicText(grouped, style = Mono.copy(fontSize = 40.sp, fontWeight = FontWeight.Bold, letterSpacing = 4.sp))
    }
}

/** The result's time: mono, the headline's second line (result.templ: font-mono, xl, ink/70). */
@Composable
fun MonoTime(text: String) {
    BasicText(text, style = Mono.copy(fontSize = 20.sp, color = Palette.Ink.copy(alpha = 0.7f)))
}

/** The three progress bars; a done bar is green, ink beside a tenant accent (WL-13). */
@Composable
fun Progress(label: String, step: Int, of: Int, accented: Boolean) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        DocketLabel(label)
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (n in 1..of) {
                val c = when {
                    n > step -> Palette.Line
                    accented -> Palette.Ink
                    else -> Palette.TappaGreen
                }
                Box(Modifier.weight(1f).height(6.dp).background(c))
            }
        }
    }
}

/** The stamp: ink word, the status colour in the double frame (tappa-brand, 2026-08-01). */
@Composable
fun Stamp(word: String, tone: Tone) {
    Box(Modifier.rotate(-8f).background(tone.color.copy(alpha = 0.1f)).border(2.dp, tone.color).padding(2.dp)
        .border(1.dp, tone.color).padding(horizontal = 8.dp, vertical = 4.dp)) {
        BasicText(word.uppercase(LocalConfiguration.current.locales[0]), style = Mono.copy(fontSize = 11.sp, fontWeight = FontWeight.Bold, letterSpacing = 2.sp))
    }
}

/**
 * The waiting screen's plaque. It stays Taptime's on a branded flow (ADR 0023 §9 item 4: the
 * plaque on the wall carries the Taptime mark), so it takes no brand. Drawn still: the browser's
 * rings move under CSS; a moving drawing here would need a repeating animation for a picture.
 */
@Composable
fun Plaque(word: String, hold: String) {
    Box(Modifier.fillMaxWidth().padding(vertical = 8.dp), contentAlignment = Alignment.Center) {
        Box(Modifier.size(196.dp).border(1.dp, Palette.TappaGreen.copy(alpha = 0.25f)), contentAlignment = Alignment.Center) {
            Column(Modifier.widthIn(min = 132.dp).background(Palette.Paper).border(1.dp, Palette.Line).padding(18.dp),
                horizontalAlignment = Alignment.CenterHorizontally) {
                BasicText(word, style = Display.copy(fontSize = 18.sp, fontWeight = FontWeight.Bold, color = Palette.TappaGreen))
                Spacer(Modifier.height(4.dp))
                BasicText(hold.uppercase(LocalConfiguration.current.locales[0]), style = Mono.copy(fontSize = 9.sp, color = Palette.Ink.copy(alpha = 0.7f), letterSpacing = 1.sp))
            }
        }
    }
}
