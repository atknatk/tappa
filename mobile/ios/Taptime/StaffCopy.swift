import Foundation
import UserNotifications

// The staff screens' words (the native shell of 2026-11-24): English, and Turkish when the phone's
// first language is Turkish -- the Android app's two languages (M16-49), here as pairs so the app
// adds no localisation machinery. Names (a person, a business, a venue) are never translated.
enum StaffCopy {
    static var turkish: Bool { Locale.preferredLanguages.first?.lowercased().hasPrefix("tr") ?? false }
    static func t(_ en: String, _ tr: String) -> String { turkish ? tr : en }
    /// The design's stamps are capitals; the words are the Android app's, capitalised by the
    /// language's own rules (Turkish i -> İ), as Android's uppercase(locale) does.
    static func upper(_ s: String) -> String { s.uppercased(with: Locale(identifier: turkish ? "tr" : "en")) }

    static var tabToday: String { t("Today", "Bugün") }
    static var tabHours: String { t("My hours", "Saatlerim") }
    static var tabSettings: String { t("Settings", "Ayarlar") }

    static func hello(_ first: String) -> String { t("Hello, \(first)", "Merhaba, \(first)") }
    static var atWork: String { upper(t("At work", "İştesin")) }
    static var notIn: String { upper(t("Not clocked in", "İşte değilsin")) }
    static func inAt(_ hm: String) -> String { t("In at \(hm)", "Giriş \(hm)") }
    static var notInBody: String { t("See you at your next shift.", "Bir sonraki vardiyada görüşürüz.") }
    static func shiftUntil(_ hm: String) -> String { t("Shift until \(hm)", "Vardiya bitişi \(hm)") }
    static func left(_ d: String) -> String { t("\(d) left", "Kalan \(d)") }
    static func over(_ d: String) -> String { t("\(d) past the shift", "Vardiyadan fazla \(d)") }
    static var tapOut: String { t("Tap a plaque to clock out", "Çıkış için plakete dokun") }
    static var tapIn: String { t("Tap a plaque to clock in", "Giriş için plakete dokun") }
    static var tapHint: String { t("Touch here, then hold your phone to the plaque.", "Buraya dokun, sonra telefonunu plakete tut.") }
    static var reading: String { t("Hold your phone to the plaque…", "Telefonunu plakete tut…") }
    static var missingOne: String { t("You have a missing time", "Eksik bir saatin var") }
    static func missingMany(_ n: Int) -> String { t("\(n) missing times", "\(n) eksik saat") }
    static var missingSub: String { t("Tell your manager the time", "Saatini müdürüne bildir") }
    static var week: String { t("Counted this week", "Bu hafta sayılan") }
    static var counted: String { t("Counted", "Sayılan") }
    static func duration(_ minutes: Int) -> String {
        let h = minutes / 60
        let m = String(format: "%02d", minutes % 60)
        return t("\(h) h \(m) min", "\(h) sa \(m) dk")
    }

    // My hours
    /// Said on Today when the employer turned My hours off while it was open (M16-33).
    static var hoursOff: String { t("Your employer has turned My hours off.", "İşvereniniz Saatlerim özelliğini kapattı.") }
    static func openChip(_ n: Int) -> String { t("\(n) open", "Açık giriş: \(n)") }
    static func awaiting(_ d: String) -> String { t("Waiting for your manager, not counted yet: \(d).", "Müdür onayı bekleyen, henüz sayılmayan: \(d).") }
    static func openNote(_ n: Int) -> String {
        t(n == 1 ? "1 check-in has no check-out yet, so it is not counted." : "\(n) check-ins have no check-out yet, so they are not counted.",
          "\(n) girişin henüz çıkışı yok; sayılmadı.")
    }
    static func inMissing(_ n: Int) -> String { t(n == 1 ? "1 check-out has no check-in." : "\(n) check-outs have no check-in.", "\(n) çıkışın girişi yok.") }
    static func waitingLine(records: Int, claims: Int) -> String {
        var parts: [String] = []
        if records > 0 { parts.append(t(records == 1 ? "1 record" : "\(records) records", "\(records) kayıt")) }
        if claims > 0 { parts.append(t(claims == 1 ? "1 time claim" : "\(claims) time claims", "\(claims) saat talebi")) }
        return t("Waiting for your manager: ", "Müdür onayı bekleyen: ") + parts.joined(separator: " · ") + "."
    }
    static var truncated: String { t("Too many records to add up: the total is at least this much.", "Toplanamayacak kadar çok kayıt var: toplam en az bu kadar.") }
    static var dayEmpty: String { t("No records this day.", "Bu gün kayıt yok.") }
    static var dirIn: String { t("In", "Giriş") }
    static var dirOut: String { t("Out", "Çıkış") }
    static func state(_ s: String) -> String {
        switch s {
        case "approved": return t("Counted", "Sayıldı")
        case "waiting": return t("Waiting for your manager", "Müdür onayı bekliyor")
        case "repeat": return t("Repeat tap", "Tekrar dokunuş")
        default: return t("Not counted", "Sayılmadı")
        }
    }
    static func channel(_ c: String) -> String? {
        switch c {
        case "manual": return t("Entered by your manager", "Müdür girdi")
        case "qr": return "QR"
        case "qr_live": return t("Live QR", "Canlı QR")
        default: return nil
        }
    }
    static func claim(_ c: String?) -> String? {
        switch c {
        case "pending": return t("Time claim sent", "Saat talebi gönderildi")
        case "approved": return t("Time claim approved", "Saat talebi onaylandı")
        case "rejected": return t("Time claim declined", "Saat talebi reddedildi")
        default: return nil
        }
    }
    static var more: String { t("Show more", "Daha fazla göster") }
    static var previous: String { t("Previous week", "Önceki hafta") }
    static var next: String { t("Next week", "Sonraki hafta") }

    // The missing-time sheet
    static func lead(kind: String, day: String, date: String, at: String) -> String {
        if kind == "in" {
            switch day {
            case "today": return t("You clocked out at \(at) but there's no clock-in.", "Çıkış saatin \(at), ama giriş yok.")
            case "yesterday": return t("Yesterday you clocked out at \(at) but there's no clock-in.", "Dün çıkış saatin \(at), ama giriş yok.")
            default: return t("On \(date) you clocked out at \(at) but there's no clock-in.", "\(date) günü çıkış saatin \(at), ama giriş yok.")
            }
        }
        switch day {
        case "today": return t("You clocked in at \(at) and didn't clock out.", "Giriş saatin \(at), ama çıkış yapmadın.")
        case "yesterday": return t("Yesterday you clocked in at \(at) but there's no clock-out.", "Dün giriş saatin \(at), ama çıkış yok.")
        default: return t("On \(date) you clocked in at \(at) but there's no clock-out.", "\(date) günü giriş saatin \(at), ama çıkış yok.")
        }
    }
    static func question(_ kind: String) -> String {
        kind == "in" ? t("What time did you come in?", "Kaçta geldin?") : t("What time did you leave?", "Kaçta çıktın?")
    }
    /// The window phrase (the browser's claimWindowPhrase, Android's ClaimWords.window): an
    /// excluded end "after" / "before", an included one "from" / "up to".
    static func window(from: ClaimsResponse.Bound, to: ClaimsResponse.Bound) -> String {
        let a = from.at, b = to.at
        switch (from.inclusive, to.inclusive) {
        case (true, true): return t("from \(a) up to \(b)", "\(a) ile \(b) arası")
        case (true, false): return t("from \(a) and before \(b)", "en erken \(a) ve \(b) öncesi")
        case (false, true): return t("after \(a) up to \(b)", "\(a) sonrası ve en geç \(b)")
        case (false, false): return t("after \(a) and before \(b)", "\(a) sonrası ve \(b) öncesi")
        }
    }
    /// The sheet's hint before any time is sent: the window alone. "Does not fit" is the server's
    /// answer to a sent time (outside).
    static func windowHint(_ window: String) -> String { t("It has to be \(window).", "Şu aralıkta olmalı: \(window).") }
    static func outside(_ window: String) -> String {
        t("That time does not fit this record. It has to be \(window).", "Bu saat bu kayda uymuyor. Şu aralıkta olmalı: \(window).")
    }
    static var send: String { t("Send to my manager", "Müdürüme gönder") }
    static var sending: String { t("Sending…", "Gönderiliyor…") }
    static var sendShort: String { t("Send", "Gönder") }
    /// The browser card's line under the send button (claim.templ), here also the control that
    /// moves to the next record (M16-44).
    static var skip: String { t("You can skip this.", "Bunu atlayabilirsin.") }
    static var sent: String { t("Sent — your manager will review it.", "Gönderildi — müdürün inceleyecek.") }
    // The outcomes are the Android app's banners: a title, " — ", then the card's lines (M16-44:
    // the browser's, without "Your tap itself is recorded." -- there is no tap on this screen).
    private static var tellManager: String { t("Tell your manager what time it was.", "Müdürüne saatin kaç olduğunu söyle.") }
    static var expired: String {
        t("This question has expired", "Bu sorunun süresi doldu") + " — "
            + t("Nothing was sent to your manager.", "Müdürüne hiçbir şey gönderilmedi.") + " " + tellManager
    }
    static var resolved: String {
        t("Already resolved", "Zaten çözülmüş") + " — "
            + t("That record no longer needs this time, so nothing was sent.", "Bu kayıt artık bu saate ihtiyaç duymuyor, bu yüzden hiçbir şey gönderilmedi.")
            + " " + t("Tell your manager if that looks wrong.", "Yanlış görünüyorsa müdürüne söyle.")
    }
    static var notActive: String {
        t("Your account can't send this", "Hesabın bunu gönderemez") + " — "
            + t("Only an active employee can send a time to their manager. Nothing was sent.",
                "Müdürüne saati yalnız aktif bir çalışan gönderebilir. Hiçbir şey gönderilmedi.")
            + " " + tellManager
    }
    static var stillOpen: String {
        t("You're still clocked in", "Hâlâ girişlisin") + " — " + t("Nothing was sent. Tap the plaque when you leave.", "Hiçbir şey gönderilmedi. Çıkarken plakete dokun.")
    }
    // The person's own claims, under the record (M16-44): Android's missing_sent_* and claim_state_*.
    static var sentLabel: String { t("Sent", "Gönderilenler") }
    static func sentRow(date: String, kind: String, at: String) -> String { t("\(date) · \(kind) \(at)", "\(date) · \(kind) \(at)") }
    static func sentKind(_ kind: String) -> String { kind == "in" ? t("in", "giriş") : t("out", "çıkış") }
    /// A sent claim's state: approvedAt is the clock the manager approved, which may differ from
    /// the one asked for (ADR 0032 §C.2); without it, just "Approved".
    static func claimState(_ state: String, approvedAt: String?) -> String {
        switch state {
        case "approved":
            if let at = approvedAt { return t("Approved at \(at)", "Onaylandı: \(at)") }
            return t("Approved", "Onaylandı")
        case "rejected": return t("Not approved", "Onaylanmadı")
        default: return t("Waiting for your manager", "Müdürün bekleniyor")
        }
    }
    static var network: String { t("Can't reach Taptime. Check your connection.", "Taptime'a ulaşılamıyor. İnternetini kontrol et.") }

    // Settings
    static var connected: String { t("This phone is connected", "Bu telefon bağlı") }
    static var notifications: String { t("Notifications", "Bildirimler") }
    static var remind: String { t("Remind me to clock out", "Çıkışı bana hatırlat") }
    static var remindHint: String {
        t("If you may have forgotten, your phone reminds you about 10 hours after you clock in, or 30 minutes after your shift ends.",
          "Unutmuş olabilirsen, telefonun girişten yaklaşık 10 saat sonra ya da vardiyan bittikten 30 dakika sonra sana hatırlatır.")
    }
    static var notificationsOff: String {
        t("Notifications are off for Taptime in your phone's Settings.", "Telefonunun Ayarlar'ında Taptime bildirimleri kapalı.")
    }
    // The lock-screen counter (M16-47): the title is the Android app's settings_timer, the
    // subtitle the design's (ios/Settings).
    static var counter: String { t("Show a timer on the lock screen", "Kilit ekranında sayaç göster") }
    static var counterSub: String { t("A live counter while you're in", "İşteyken canlı sayaç") }
    static var activitiesOff: String {
        t("Live Activities are off for Taptime in your phone's Settings.", "Telefonunun Ayarlar'ında Taptime için Canlı Etkinlikler kapalı.")
    }
    static var shift: String { t("Shift", "Vardiya") }
    static var usualShift: String { t("Usual shift", "Her zamanki vardiya") }
    /// The usual shift as a sentence (M16-48; Android's shift_usual*): the server's clocks and
    /// venue; a night shift says "(next day)" after its end; no home venue (K-14), no " · ".
    static func usualShiftLine(_ s: UsualShift) -> String {
        switch (s.overnight, s.venue.isEmpty) {
        case (true, false): return t("Usual shift: \(s.start)–\(s.end) (next day) · \(s.venue)", "Olağan vardiya: \(s.start)–\(s.end) (ertesi gün) · \(s.venue)")
        case (false, false): return t("Usual shift: \(s.start)–\(s.end) · \(s.venue)", "Olağan vardiya: \(s.start)–\(s.end) · \(s.venue)")
        case (true, true): return t("Usual shift: \(s.start)–\(s.end) (next day)", "Olağan vardiya: \(s.start)–\(s.end) (ertesi gün)")
        case (false, true): return t("Usual shift: \(s.start)–\(s.end)", "Olağan vardiya: \(s.start)–\(s.end)")
        }
    }
    /// Settings' row: "(next day)" under the clocks, for a night shift only.
    static func nextDay(_ s: UsualShift) -> String? { s.overnight ? t("(next day)", "(ertesi gün)") : nil }
    static var thisPhone: String { t("This phone", "Bu telefon") }
    static var language: String { t("Language", "Dil") }
    static var languageSub: String { t("The phone's language, or one you choose", "Telefonun dili ya da seçtiğin bir dil") }
    static var signOut: String { t("Sign this phone out", "Bu telefonun bağlantısını kaldır") }
    static var signOutTitle: String { t("Sign this phone out?", "Bağlantı kaldırılsın mı?") }
    static var signOutBody: String {
        t("To use Taptime on it again you'll need a new invitation from your manager. Your records stay as they are.",
          "Taptime'ı bu telefonda yeniden kullanmak için müdüründen yeni bir davet gerekir. Kayıtların olduğu gibi kalır.")
    }
    static var signOutConfirm: String { t("Sign out", "Bağlantıyı kaldır") }
    static var failed: String { t("Couldn't do that. Check your connection and try again.", "Yapılamadı. İnternetini kontrol edip yeniden dene.") }
    static var delete: String { t("Delete my account", "Hesabımı sil") }
    static var deleteSub: String { t("Signs you out everywhere and tells your employer", "Her yerde oturumu kapatır ve işverenine bildirir") }
    static var deleteTitle: String { t("Delete your account?", "Hesabın silinsin mi?") }
    /// M16-38: a deactivated person's request signs nothing out (the server keeps their phones,
    /// M16-37), so their first sentence is "Your request goes to <Business>." instead.
    static func deleteBody(_ business: String, deactivated: Bool = false) -> String {
        let b = business.isEmpty ? t("your employer", "işletmen") : business
        if deactivated {
            return t("Your request goes to \(b). Your clock-in records are not deleted: they are legal records \(b) keeps, and your request goes to them. If you still work there, you can be invited again later.",
                     "Talebin işverenine (\(b)) iletilir. Giriş-çıkış kayıtların silinmez: bunlar işverenin tuttuğu hukuki kayıtlardır. Orada çalışmaya devam ediyorsan sonra yeniden davet edilebilirsin.")
        }
        return t("This signs you out on every phone and removes this phone's key. Your clock-in records are not deleted: they are legal records \(b) keeps, and your request goes to them. If you still work there, you can be invited again later.",
                 "Bu işlem bütün telefonlarda oturumunu kapatır ve bu telefonun anahtarını siler. Giriş-çıkış kayıtların silinmez: bunlar işverenin tuttuğu hukuki kayıtlardır. Talebin işverenine (\(b)) iletilir. Orada çalışmaya devam ediyorsan sonra yeniden davet edilebilirsin.")
    }
    static var deleteConfirm: String { t("Send the request", "Talebi gönder") }
    static var cancel: String { t("Cancel", "Vazgeç") }
    static var privacy: String {
        t("Your location is read only at the moment you tap a plaque. No finger scans, no face scans.",
          "Konumun yalnız plakete dokunduğun anda okunur. Parmak izi ya da yüz kullanılmaz.")
    }
    /// The privacy notice's link (M16-38; App Store 5.1.1(i)): opens /legal/privacy in Safari.
    static var privacyPolicy: String { t("Privacy policy", "Gizlilik politikası") }
    static func version(_ v: String) -> String { t("Version \(v)", "Sürüm \(v)") }

    // The reminder
    static var remindTitle: String { t("Still at work?", "Hâlâ işte misin?") }
    static func remindBody(_ inAt: String) -> String { t("You clocked in at \(inAt). If you've finished, remember to clock out.", "Giriş saatin \(inAt). Bitirdiysen çıkış yapmayı unutma.") }
    static func remindBodyShift(_ end: String) -> String { t("If you've finished, remember to clock out. Your shift ended at \(end).", "Bitirdiysen çıkış yapmayı unutma. Vardiyanın bitişi: \(end).") }
    static var snooze: String { t("Remind me in an hour", "1 saat sonra hatırlat") }
}

/// The local "Still at work?" reminder (M16-46's iOS half): ONE pending notification request, put
/// at /me's remindAt, with "Remind me in an hour" while that is before the business day's end.
/// No push, no location, nothing leaves the phone.
final class StaffNotifier: NSObject, UNUserNotificationCenterDelegate, ReminderScheduling {
    static let shared = StaffNotifier()
    private static let id = "taptime.remind"
    private static let category = "taptime.remind"
    private static let snoozeAction = "taptime.snooze"
    /// The reminder's clip before staleAt (M16-45's remindBeforeStale).
    private static let clip: TimeInterval = 30 * 60

    static var enabled: Bool {
        get { UserDefaults.standard.bool(forKey: "staff.remind") }
        set { UserDefaults.standard.set(newValue, forKey: "staff.remind") }
    }

    func install() {
        let c = UNUserNotificationCenter.current()
        c.delegate = self
        let snooze = UNNotificationAction(identifier: StaffNotifier.snoozeAction, title: StaffCopy.snooze, options: [])
        c.setNotificationCategories([UNNotificationCategory(identifier: StaffNotifier.category, actions: [snooze], intentIdentifiers: [], options: [])])
    }

    /// Asks once, when the switch is turned on (never at the tap): the answer.
    func ask() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])) ?? false
    }

    func notificationsAllowed() async -> Bool {
        let s = await UNUserNotificationCenter.current().notificationSettings()
        return s.authorizationStatus == .authorized || s.authorizationStatus == .provisional
    }

    /// Follows /me: a presence with a reminder schedules it, anything else takes it away.
    func apply(_ me: MeResponse?, offset: TimeInterval) {
        cancel()
        guard StaffNotifier.enabled, let p = me?.presence, let remind = Presence.instant(p.remindAt),
              let stale = Presence.instant(p.staleAt) else {
            return
        }
        let at = remind.addingTimeInterval(-offset)
        let last = stale.addingTimeInterval(-offset - StaffNotifier.clip)
        schedule(at: at, inLocal: p.inSinceLocal, shiftEnd: p.shiftEndsAtLocal, last: last)
    }

    func cancel() {
        UNUserNotificationCenter.current().removePendingNotificationRequests(withIdentifiers: [StaffNotifier.id])
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [StaffNotifier.id])
    }

    private func schedule(at: Date, inLocal: String, shiftEnd: String?, last: Date) {
        let wait = at.timeIntervalSinceNow
        guard wait > 0 else { return }
        let content = UNMutableNotificationContent()
        content.title = StaffCopy.remindTitle
        content.body = shiftEnd.map(StaffCopy.remindBodyShift) ?? StaffCopy.remindBody(inLocal)
        content.sound = .default
        content.userInfo = ["in": inLocal, "shiftEnd": shiftEnd ?? "", "last": last.timeIntervalSince1970]
        if Date().addingTimeInterval(wait + 3600) <= last {
            content.categoryIdentifier = StaffNotifier.category
        }
        let trigger = UNTimeIntervalNotificationTrigger(timeInterval: wait, repeats: false)
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: StaffNotifier.id, content: content, trigger: trigger))
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        defer { done() }
        guard response.actionIdentifier == StaffNotifier.snoozeAction else { return }
        let info = response.notification.request.content.userInfo
        guard let inLocal = info["in"] as? String, let lastS = info["last"] as? Double else { return }
        let shiftEnd = (info["shiftEnd"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        let last = Date(timeIntervalSince1970: lastS)
        let at = Date().addingTimeInterval(3600)
        if at <= last {
            schedule(at: at, inLocal: inLocal, shiftEnd: shiftEnd, last: last)
        }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .sound])
    }
}
