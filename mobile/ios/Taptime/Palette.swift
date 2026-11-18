import SwiftUI

// The brand palette (skill tappa-brand), copied as sRGB components so the app stays inside the
// same nine tokens the web uses. Only the tokens a screen draws today live here; add one when a
// screen needs it, never a new hex.
enum Palette {
    static let ink = Color(red: 0x15 / 255, green: 0x22 / 255, blue: 0x19 / 255)
    static let porcelain = Color(red: 0xED / 255, green: 0xF0 / 255, blue: 0xEA / 255)
    static let green = Color(red: 0x1F / 255, green: 0x5C / 255, blue: 0x41 / 255)
}
