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
    /// The time the server last answered "outside_window" to: its "does not fit" is about THAT
    /// time, so it stands only while the wheel still shows it.
    @Published private(set) var claimRefusedAt: String?
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
    /// The lock-screen counter's switch (M16-47; iOS 16.2+ only) and whether the system's own
    /// Live Activities switch for Taptime is off.
    @Published var counter = LiveCounter.enabled
    @Published private(set) var activitiesOff = false
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
        do {
            week = try await tap.api.hours(id, week: nil, after: nil)
        } catch AppAPIError.unauthenticated {
            signedOut()
        } catch {
            // The summary card simply waits for the next read.
        }
    }

    /// Any staff read answered 401 (after the clock retry) takes the home screen's path: the
    /// reminder, the widget and the counter are cleared and the activation screens say "This phone
    /// was signed out" (the Android twin: StaffFlow.Answer.SignedOut -> clearPresence +
    /// activationEntry(SIGNED_OUT)). Nothing of the sheet survives into the next activation.
    private func signedOut() {
        claims = nil
        claimIndex = 0
        claimProblem = nil
        claimRefusedAt = nil
        tap.staffReadSignedOut()
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
            var cursor = after
            let r: HoursResponse
            do {
                r = try await tap.api.hours(id, week: week, after: cursor)
            } catch let e as AppAPIError where cursor != nil && StaffModel.cursorRefused(e) {
                // "Show more" refused: the cursor is no longer this person's and week's (an older
                // shape, another walk). Page one of the same week again, quietly, replacing the list
                // -- the same cursor again would be the same 400.
                cursor = nil
                r = try await tap.api.hours(id, week: week, after: nil)
            }
            records = cursor == nil ? r.records : records + r.records
            hours = r
        } catch AppAPIError.refused(.notFound, _) {
            hoursOff = true
            hours = nil
        } catch AppAPIError.unauthenticated {
            signedOut()
        } catch {
            hoursFailed = true
        }
    }

    /// A 400 to a signed read: bad_request, or a code this version does not know.
    nonisolated static func cursorRefused(_ e: AppAPIError) -> Bool {
        switch e {
        case .refused(let code, _): return code == .badRequest
        case .unexpected(status: 400): return true
        default: return false
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
            claimRefusedAt = nil
        } catch AppAPIError.unauthenticated {
            signedOut()
        } catch {
            notice = StaffCopy.network
        }
    }

    func closeSheet() {
        claims = nil
        claimProblem = nil
        claimRefusedAt = nil
    }

    func skip() {
        guard let list = claims else { return }
        if claimIndex + 1 < list.count {
            claimIndex += 1
            claimProblem = nil
            claimRefusedAt = nil
        } else {
            closeSheet()
        }
    }

    func send(_ c: ClaimsResponse.Open, at: String) async {
        guard let id = tap.identity, !claimBusy else { return }
        claimBusy = true
        claimProblem = nil
        claimRefusedAt = nil
        defer { claimBusy = false }
        let word: String
        do {
            word = try await tap.api.claimSend(id, context: c.context, at: at)
        } catch AppAPIError.refused(.staleContext, _) {
            await reloadClaims(notice: nil)
            return
        } catch AppAPIError.unauthenticated {
            signedOut()
            return
        } catch {
            claimProblem = StaffCopy.network
            return
        }
        switch word {
        case "sent", "already_sent": await reloadClaims(notice: StaffCopy.sent)
        case "outside_window":
            claimProblem = StaffCopy.outside(StaffCopy.window(from: c.windowFrom, to: c.windowTo))
            claimRefusedAt = at
        case "expired": await reloadClaims(notice: StaffCopy.expired)
        case "resolved": await reloadClaims(notice: StaffCopy.resolved)
        case "not_active": claimProblem = StaffCopy.notActive
        case "still_open": await reloadClaims(notice: StaffCopy.stillOpen)
        default: claimProblem = StaffCopy.network
        }
    }

    /// The sheet's footer: the window until a sent time is refused; then the refusal, while the
    /// wheel still shows that time. Other problems (the network, an inactive account) stand.
    func claimHint(_ c: ClaimsResponse.Open, picked: String) -> (text: String, problem: Bool) {
        if let p = claimProblem, claimRefusedAt == nil || claimRefusedAt == picked {
            return (p, true)
        }
        return (StaffCopy.windowHint(StaffCopy.window(from: c.windowFrom, to: c.windowTo)), false)
    }

    private func reloadClaims(notice: String?) async {
        guard let id = tap.identity else { return }
        let r: ClaimsResponse?
        do {
            r = try await tap.api.claims(id)
        } catch AppAPIError.unauthenticated {
            signedOut()
            return
        } catch {
            r = nil
        }
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
        counter = LiveCounter.enabled
        activitiesOff = LiveCounter.supported && !PresenceSync.shared.counterAllowed
    }

    /// No permission prompt: a local Live Activity has none -- the system's per-app switch is the
    /// person's, and the row says when it is off.
    func setCounter(_ on: Bool) {
        LiveCounter.enabled = on
        counter = on
        activitiesOff = LiveCounter.supported && !PresenceSync.shared.counterAllowed
        PresenceSync.shared.counterSwitched()
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
