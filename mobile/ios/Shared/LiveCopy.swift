import Foundation

// The lock screen's and the home-screen widget's words (M16-47; compiled into BOTH targets). The
// StaffCopy pattern: English, and Turkish when the phone's first language is Turkish. Every pair
// is the Android app's strings.xml / values-tr line (timer_title, timer_shift_ends, today_*,
// widget_description) -- IosCopyTest holds this file to them both ways, so a line Android has not
// got ("Open Taptime to check": Android's timer simply ends at showUntil) is named there. No name
// of any kind: the lock screen is seen by whoever stands next to the phone (Q53 (2) (a)).
enum LiveCopy {
    static var turkish: Bool { Locale.preferredLanguages.first?.lowercased().hasPrefix("tr") ?? false }
    static func t(_ en: String, _ tr: String) -> String { turkish ? tr : en }
    /// The design's stamps are capitals; the words are Android's, capitalised by the language's
    /// own rules (Turkish i -> İ).
    static func upper(_ s: String) -> String { s.uppercased(with: Locale(identifier: turkish ? "tr" : "en")) }

    static let wordmark = "taptime"
    static var clockedIn: String { upper(t("Clocked in", "Giriş yapıldı")) }
    static func inAt(_ hm: String) -> String { t("In at \(hm)", "Giriş \(hm)") }
    static func shiftEnds(_ hm: String) -> String { t("Shift ends \(hm)", "Vardiya bitişi \(hm)") }
    static var atWork: String { upper(t("At work", "İştesin")) }
    static var notIn: String { upper(t("Not clocked in", "İşte değilsin")) }
    static var tapIn: String { t("Tap a plaque to clock in", "Giriş için plakete dokun") }
    /// After showUntil, or with nothing synced: the phone cannot know (a browser clock-out never
    /// reaches it), so it says so instead of counting (M16-47).
    static var check: String { t("Open Taptime to check", "Kontrol için Taptime'ı aç") }
    static var widgetName: String { "Taptime" }
    static var widgetDescription: String { t("At work, and since when", "İşte misin, ne zamandan beri") }
}
