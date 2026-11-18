import SwiftUI

// The brand palette (skill tappa-brand), copied as sRGB components so the app stays inside the
// same nine tokens the web uses. Only the tokens a screen draws today live here; add one when a
// screen needs it, never a new hex.
enum Palette {
    static let ink = Color(red: 0x15 / 255, green: 0x22 / 255, blue: 0x19 / 255)
    static let porcelain = Color(red: 0xED / 255, green: 0xF0 / 255, blue: 0xEA / 255)
    static let paper = Color(red: 0xFF / 255, green: 0xFD / 255, blue: 0xF4 / 255)
    static let green = Color(red: 0x1F / 255, green: 0x5C / 255, blue: 0x41 / 255)
    static let saffron = Color(red: 0xD9 / 255, green: 0x8E / 255, blue: 0x2B / 255)
    static let tomato = Color(red: 0xBE / 255, green: 0x3D / 255, blue: 0x2A / 255)
    static let line = Color(red: 0xC9 / 255, green: 0xD2 / 255, blue: 0xC8 / 255)
}

// THE BUSINESS'S ACCENT (ADR 0023 §2-§4; skill tappa-brand "Tenant slotları"). The server sends an
// accent only after its WCAG gate passed it; the app still re-derives the two things the web's
// theme stylesheet carries -- the button text colour (paper or ink, whichever contrasts more) and
// the 2 px ink edge when the accent is under 3:1 on porcelain -- because it has no stylesheet to
// receive them from. The accent fills ONE slot on these screens: the primary button of steps 1-3.
// With an accent, the wizard's other green marks (progress bar, step numbers, the wordmark) are ink.
struct BrandLook: Equatable {
    struct RGB: Equatable {
        let r: Double, g: Double, b: Double

        var color: Color { Color(red: r, green: g, blue: b) }

        // WCAG 2.x relative luminance.
        var luminance: Double {
            func lin(_ c: Double) -> Double { c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4) }
            return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
        }

        static func hex(_ s: String) -> RGB? {
            guard s.utf8.count == 6, s.utf8.allSatisfy({ ($0 >= 48 && $0 <= 57) || ($0 >= 65 && $0 <= 70) || ($0 >= 97 && $0 <= 102) }),
                  let v = UInt32(s, radix: 16) else {
                return nil
            }
            return RGB(r: Double((v >> 16) & 0xFF) / 255, g: Double((v >> 8) & 0xFF) / 255, b: Double(v & 0xFF) / 255)
        }

        static let ink = RGB(r: 0x15 / 255.0, g: 0x22 / 255.0, b: 0x19 / 255.0)
        static let paper = RGB(r: 0xFF / 255.0, g: 0xFD / 255.0, b: 0xF4 / 255.0)
        static let porcelain = RGB(r: 0xED / 255.0, g: 0xF0 / 255.0, b: 0xEA / 255.0)
    }

    static func contrast(_ a: RGB, _ b: RGB) -> Double {
        let (x, y) = (a.luminance, b.luminance)
        return (max(x, y) + 0.05) / (min(x, y) + 0.05)
    }

    /// nil = Taptime's own look (green button, green wordmark).
    let accent: RGB?

    init(_ brand: AppBrand) {
        accent = brand.accent.flatMap(RGB.hex)
    }

    var accented: Bool { accent != nil }

    var buttonFill: Color { accent?.color ?? Palette.green }

    var buttonText: Color {
        guard let a = accent else {
            return Palette.paper
        }
        return BrandLook.contrast(a, .paper) >= BrandLook.contrast(a, .ink) ? Palette.paper : Palette.ink
    }

    /// The 2 px ink edge of an accent the page background would swallow (WCAG 1.4.11, 3:1).
    var buttonEdge: Bool {
        guard let a = accent else {
            return false
        }
        return BrandLook.contrast(a, .porcelain) < 3
    }

    /// The progress bar, the step numbers and the wordmark: green, or ink beside an accent.
    var mark: Color { accented ? Palette.ink : Palette.green }
}
