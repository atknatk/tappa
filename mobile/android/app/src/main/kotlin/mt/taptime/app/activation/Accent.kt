package mt.taptime.app.activation

import kotlin.math.pow

// The tenant accent's fill, the app's twin of internal/brand's OnColor and Edge (ADR 0023 §3).
// The SERVER gates the accent (brand.Check) before it ever reaches a reply; this side only
// paints it, with the same two derived choices the browser's theme stylesheet makes:
//   * the label on the accent: paper or ink, whichever contrasts more (a tie goes to paper);
//   * a 2 px ink edge when the accent is below 3:1 against the porcelain page.
// A value that is not exactly six hex digits is no accent at all: Taptime's look.
data class AccentFill(val fill: Int, val label: Int, val edge: Boolean) {
    companion object {
        const val INK = 0x152219
        const val PAPER = 0xFFFDF4
        const val PORCELAIN = 0xEDF0EA
        private const val EDGE_MINIMUM = 3.0

        fun of(accent: String?): AccentFill? {
            if (accent == null || accent.length != 6 || accent.any { it !in '0'..'9' && it !in 'A'..'F' && it !in 'a'..'f' }) {
                return null
            }
            val rgb = accent.toInt(16)
            val label = if (contrast(PAPER, rgb) >= contrast(INK, rgb)) PAPER else INK
            return AccentFill(rgb, label, contrast(rgb, PORCELAIN) < EDGE_MINIMUM)
        }

        /** WCAG 2.x relative luminance over the linearised sRGB channels. */
        fun luminance(rgb: Int): Double {
            fun lin(v: Int): Double {
                val c = v / 255.0
                return if (c <= 0.04045) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)
            }
            return 0.2126 * lin(rgb shr 16 and 0xff) + 0.7152 * lin(rgb shr 8 and 0xff) + 0.0722 * lin(rgb and 0xff)
        }

        fun contrast(a: Int, b: Int): Double {
            val la = luminance(a)
            val lb = luminance(b)
            return (maxOf(la, lb) + 0.05) / (minOf(la, lb) + 0.05)
        }
    }
}
