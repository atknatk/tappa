import SwiftUI

// The staff screens' state (the native shell of 2026-11-24, the Android app's twin): three tabs --
// Today, My hours, Settings -- and the missing-time sheet. It reads with the device TapModel already
// holds; it writes only a claim, a sign-out and a deletion request. Nothing it reads is stored on the
// phone (My hours stay off the device, M16-33).
@MainActor
final class StaffModel: ObservableObject {
    enum Tab { case today, hours, settings }

    enum Confirm { case signOut, delete }

    @Published var tab: Tab = .today
    // Today
    @Published private(set) var week: HoursResponse?
    @Published private(set) var claims: [ClaimsResponse.Open]?
    @Published private(set) var claimIndex = 0
    @Published private(set) var claimBusy = false
    @Published private(set) var claimProblem: String?
    @Published var notice: String?
    // My hours
    @Published private(set) var hours: HoursResponse?
    @Published private(set) var records: [HoursResponse.Record] = []
    @Published private(set) var hoursBusy = false
    @Published private(set) var hoursFailed = false
    @Published private(set) var hoursOff = false
    // Settings
    @Published var remind = StaffNotifier.enabled
    @Published private(set) var notificationsOff = false
    @Published var confirm: Confirm?
    @Published private(set) var confirmBusy = false
    @Published private(set) var confirmFailed = false

    let tap: TapModel
    /// The person's own sign-out (either act): back to activation.
    var onForgotten: (() -> Void)?

    init(tap: TapModel) {
        self.tap = tap
    }

    private var me: MeResponse? { tap.me }

    // MARK: Tabs

    func show(_ t: Tab) {
        tab = t
        switch t {
        case .today: Task { await refreshToday() }
        case .hours: Task { await loadHours(week: nil, after: nil) }
        case .settings: Task { await refreshNotificationState() }
        }
    }

    func refreshToday() async {
        guard let id = tap.identity, (me?.hoursWeeks ?? 0) > 0 else {
            week = nil
            return
        }
        if let w = try? await tap.api.hours(id, week: nil, after: nil) {
            week = w
        }
    }

    // MARK: My hours

    func loadHours(week: String?, after: String?) async {
        guard let id = tap.identity else { return }
        hoursOff = (me?.hoursWeeks ?? 0) <= 0
        guard !hoursOff else { return }
        hoursBusy = true
        hoursFailed = false
        defer { hoursBusy = false }
        do {
            let r = try await tap.api.hours(id, week: week, after: after)
            records = after == nil ? r.records : records + r.records
            hours = r
        } catch AppAPIError.refused(.notFound, _) {
            hoursOff = true
            hours = nil
        } catch {
            hoursFailed = true
        }
    }

    func shiftWeek(_ days: Int) {
        guard let h = hours, let d = StaffModel.day(h.week), let moved = Calendar(identifier: .gregorian).date(byAdding: .day, value: days, to: d) else {
            return
        }
        Task { await loadHours(week: StaffModel.dayString(moved), after: nil) }
    }

    nonisolated static func day(_ s: String) -> Date? {
        let f = DateFormatter()
        f.calendar = Calendar(identifier: .gregorian)
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone(identifier: "UTC")
        f.dateFormat = "yyyy-MM-dd"
        return f.date(from: s)
    }

    nonisolated static func dayString(_ d: Date) -> String {
        let f = DateFormatter()
        f.calendar = Calendar(identifier: .gregorian)
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone(identifier: "UTC")
        f.dateFormat = "yyyy-MM-dd"
        return f.string(from: d)
    }

    // MARK: The missing-time sheet

    func openMissing() async {
        guard let id = tap.identity, !claimBusy else { return }
        claimBusy = true
        defer { claimBusy = false }
        do {
            let r = try await tap.api.claims(id)
            claims = r.open.isEmpty ? nil : r.open
            claimIndex = 0
            claimProblem = nil
        } catch {
            notice = StaffCopy.network
        }
    }

    func closeSheet() {
        claims = nil
        claimProblem = nil
    }

    func skip() {
        guard let list = claims else { return }
        if claimIndex + 1 < list.count {
            claimIndex += 1
            claimProblem = nil
        } else {
            closeSheet()
        }
    }

    func send(_ c: ClaimsResponse.Open, at: String) async {
        guard let id = tap.identity, !claimBusy else { return }
        claimBusy = true
        claimProblem = nil
        defer { claimBusy = false }
        let word: String
        do {
            word = try await tap.api.claimSend(id, context: c.context, at: at)
        } catch AppAPIError.refused(.staleContext, _) {
            await reloadClaims(notice: nil)
            return
        } catch {
            claimProblem = StaffCopy.network
            return
        }
        switch word {
        case "sent", "already_sent": await reloadClaims(notice: StaffCopy.sent)
        case "outside_window": claimProblem = StaffCopy.outside + " " + StaffCopy.window(from: c.windowFrom.at, to: c.windowTo.at)
        case "expired": await reloadClaims(notice: StaffCopy.expired)
        case "resolved": await reloadClaims(notice: StaffCopy.resolved)
        case "not_active": claimProblem = StaffCopy.notActive
        case "still_open": await reloadClaims(notice: StaffCopy.stillOpen)
        default: claimProblem = StaffCopy.network
        }
    }

    private func reloadClaims(notice: String?) async {
        guard let id = tap.identity else { return }
        let r = try? await tap.api.claims(id)
        let open = r?.open ?? []
        claims = open.isEmpty ? nil : open
        claimIndex = 0
        self.notice = notice
        await tap.home() // /me again: the missing-times count and the presence follow
    }

    // MARK: Settings

    func setRemind(_ on: Bool) async {
        if on {
            let granted = await StaffNotifier.shared.ask()
            StaffNotifier.enabled = granted
            remind = granted
            notificationsOff = !granted
        } else {
            StaffNotifier.enabled = false
            remind = false
            notificationsOff = false
        }
        StaffNotifier.shared.apply(me, offset: tap.api.clock.currentOffset)
    }

    func refreshNotificationState() async {
        remind = StaffNotifier.enabled
        notificationsOff = StaffNotifier.enabled ? !(await StaffNotifier.shared.notificationsAllowed()) : false
    }

    func perform(_ c: Confirm) async {
        guard let id = tap.identity, !confirmBusy else { return }
        confirmBusy = true
        confirmFailed = false
        defer { confirmBusy = false }
        do {
            switch c {
            case .signOut: try await tap.api.revoke(id)
            case .delete: try await tap.api.deleteAccount(id)
            }
        } catch AppAPIError.unauthenticated {
            // Already gone on the server: the phone forgets it all the same.
        } catch {
            // The system dialog closed with the press: it opens again, saying it failed.
            confirmFailed = true
            confirm = c
            return
        }
        StaffNotifier.shared.cancel()
        confirm = nil
        onForgotten?()
    }
}
