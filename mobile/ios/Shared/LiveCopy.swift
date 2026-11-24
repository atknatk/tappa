import Foundation

// The lock screen's and the home-screen widget's words (M16-47; compiled into BOTH targets). The
// StaffCopy pattern: English, and Turkish when the phone's first language is Turkish. The pairs
// are the Android app's where it has them (timer_title, timer_shift_ends) and StaffCopy's
// otherwise, so the two apps say the same thing. No name of any kind: the lock screen is seen by
// whoever stands next to the phone (Q53 (2) (a)).
enum LiveCopy {
    static var turkish: Bool { Locale.preferredLanguages.first?.lowercased().hasPrefix("tr") ?? false }
    static func t(_ en: String, _ tr: String) -> String { turkish ? tr : en }

    static let wordmark = "taptime"
    static var clockedIn: String { t("CLOCKED IN", "GİRİŞ YAPILDI") }
    static func inAt(_ hm: String) -> String { t("In at \(hm)", "Giriş \(hm)") }
    static func shiftEnds(_ hm: String) -> String { t("Shift ends \(hm)", "Vardiya bitişi \(hm)") }
    static var atWork: String { t("AT WORK", "İŞTESİN") }
    static var notIn: String { t("NOT CLOCKED IN", "İŞTE DEĞİLSİN") }
    static var tapIn: String { t("Tap a plaque to clock in", "Giriş için plakete dokun") }
    /// After showUntil, or with nothing synced: the phone cannot know (a browser clock-out never
    /// reaches it), so it says so instead of counting (M16-47).
    static var check: String { t("Open Taptime to check", "Kontrol için Taptime'ı aç") }
    static var widgetName: String { "Taptime" }
    static var widgetDescription: String { t("Whether you're clocked in, and since when.", "İşte olup olmadığın ve ne zamandan beri.") }
}
